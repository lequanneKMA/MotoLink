// Package config loads process configuration from environment variables
// (docs/design/13 §2.1). Validation is strict: misconfiguration fails at start.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Roles selects which parts of the monolith run (ADR-001).
type Roles struct{ API, WS, Worker bool }

// Config is the full process configuration.
type Config struct {
	Env         string // dev | test | prod
	Roles       Roles
	HTTPAddr    string
	MetricsAddr string

	DatabaseURL string
	RedisAddr   string
	RedisPass   string

	KafkaBrokers []string
	HistorySink  string // kafka | direct_pg   (ADR-007, ADR-021)
	OutboxSink   string // kafka | inproc      (ADR-021)

	JWTPrivateKeyFile string
	JWTKeyID          string
	AccessTTL         time.Duration
	RefreshTTL        time.Duration
	Argon2Concurrency int

	TrustedProxyHops int // number of reverse proxies whose X-Forwarded-For entry is trusted (0 = none)

	WSMaxConns      int
	WSMaxConnsPerIP int

	LogLevel       string
	DebugLogCoords bool

	FCMCredentialsFile string // service-account JSON; empty = push disabled (in-app notifications only)
	FCMProjectID       string

	RetentionFullresDays   int // raw GPS points are kept this many days after a session ended (docs/design/05 §6)
	RetentionPartitionDays int // whole monthly partitions older than this are dropped

	OSRMURL            string // osrm-routed base URL; empty disables routing
	TileBaseURL        string // tile server (Martin) base URL as clients reach it; empty disables the self-hosted basemap
	GeocodeDatabaseURL string // PostgreSQL with the OSM `places` table; empty disables place search

	ShutdownTimeout time.Duration
}

// Load reads the environment. lookup is os.LookupEnv in production; tests inject a map.
func Load(lookup func(string) (string, bool)) (Config, error) {
	g := getter{lookup: lookup}
	c := Config{
		Env:                    g.str("APP_ENV", "dev"),
		HTTPAddr:               g.str("HTTP_ADDR", ":8080"),
		MetricsAddr:            g.str("METRICS_ADDR", ":9100"),
		DatabaseURL:            g.str("DATABASE_URL", ""),
		RedisAddr:              g.str("REDIS_ADDR", ""),
		RedisPass:              g.str("REDIS_PASSWORD", ""),
		HistorySink:            g.str("HISTORY_SINK", "kafka"),
		OutboxSink:             g.str("OUTBOX_SINK", "kafka"),
		JWTPrivateKeyFile:      g.str("JWT_ED25519_PRIVATE_KEY_FILE", ""),
		JWTKeyID:               g.str("JWT_KID", "k1"),
		AccessTTL:              g.dur("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTTL:             g.dur("REFRESH_TOKEN_TTL", 60*24*time.Hour),
		Argon2Concurrency:      g.integer("ARGON2_MAX_CONCURRENCY", 2),
		TrustedProxyHops:       g.integer("TRUSTED_PROXY_HOPS", 0),
		WSMaxConns:             g.integer("WS_MAX_CONNS", 1500),
		WSMaxConnsPerIP:        g.integer("WS_MAX_CONNS_PER_IP", 20),
		LogLevel:               g.str("LOG_LEVEL", "info"),
		DebugLogCoords:         g.boolean("DEBUG_LOG_COORDS", false),
		ShutdownTimeout:        g.dur("SHUTDOWN_TIMEOUT", 25*time.Second),
		FCMCredentialsFile:     g.str("FCM_CREDENTIALS_FILE", ""),
		FCMProjectID:           g.str("FCM_PROJECT_ID", ""),
		RetentionFullresDays:   g.integer("RETENTION_FULLRES_DAYS", 30),
		RetentionPartitionDays: g.integer("RETENTION_PARTITION_DAYS", 90),
	}
	if b := g.str("KAFKA_BROKERS", ""); b != "" {
		for _, s := range strings.Split(b, ",") {
			if s = strings.TrimSpace(s); s != "" {
				c.KafkaBrokers = append(c.KafkaBrokers, s)
			}
		}
	}
	roles, err := parseRoles(g.str("ROLES", "api,ws,worker"))
	if err != nil {
		g.errs = append(g.errs, err)
	}
	c.Roles = roles
	if len(g.errs) > 0 {
		return Config{}, errors.Join(g.errs...)
	}
	return c, c.Validate()
}

// LoadEnv loads from the real process environment.
func LoadEnv() (Config, error) { return Load(os.LookupEnv) }

// Validate checks cross-field rules.
func (c Config) Validate() error {
	var errs []error
	if c.Env != "dev" && c.Env != "test" && c.Env != "prod" {
		errs = append(errs, fmt.Errorf("APP_ENV must be dev|test|prod, got %q", c.Env))
	}
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.HistorySink != "kafka" && c.HistorySink != "direct_pg" {
		errs = append(errs, fmt.Errorf("HISTORY_SINK must be kafka|direct_pg, got %q", c.HistorySink))
	}
	if c.OutboxSink != "kafka" && c.OutboxSink != "inproc" {
		errs = append(errs, fmt.Errorf("OUTBOX_SINK must be kafka|inproc, got %q", c.OutboxSink))
	}
	if (c.HistorySink == "kafka" || c.OutboxSink == "kafka") && c.Roles.needsKafka() && len(c.KafkaBrokers) == 0 {
		errs = append(errs, errors.New("KAFKA_BROKERS is required when HISTORY_SINK or OUTBOX_SINK is kafka"))
	}
	if c.Env == "prod" && c.DebugLogCoords {
		errs = append(errs, errors.New("DEBUG_LOG_COORDS must not be enabled when APP_ENV=prod (docs/design/11 §4.5)"))
	}
	if c.TrustedProxyHops < 0 || c.TrustedProxyHops > 5 {
		errs = append(errs, errors.New("TRUSTED_PROXY_HOPS must be 0..5"))
	}
	if c.Argon2Concurrency < 1 {
		errs = append(errs, errors.New("ARGON2_MAX_CONCURRENCY must be >= 1"))
	}
	if c.AccessTTL <= 0 || c.RefreshTTL <= c.AccessTTL {
		errs = append(errs, errors.New("token TTLs invalid: need 0 < ACCESS_TOKEN_TTL < REFRESH_TOKEN_TTL"))
	}
	return errors.Join(errs...)
}

func (r Roles) needsKafka() bool { return r.WS || r.Worker }

func parseRoles(s string) (Roles, error) {
	var r Roles
	for _, p := range strings.Split(s, ",") {
		switch strings.TrimSpace(p) {
		case "api":
			r.API = true
		case "ws":
			r.WS = true
		case "worker":
			r.Worker = true
		case "":
		default:
			return Roles{}, fmt.Errorf("ROLES: unknown role %q (api|ws|worker)", p)
		}
	}
	if !r.API && !r.WS && !r.Worker {
		return Roles{}, errors.New("ROLES: at least one role required")
	}
	return r, nil
}

type getter struct {
	lookup func(string) (string, bool)
	errs   []error
}

func (g *getter) str(k, def string) string {
	if v, ok := g.lookup(k); ok && v != "" {
		return v
	}
	return def
}

func (g *getter) integer(k string, def int) int {
	v, ok := g.lookup(k)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		g.errs = append(g.errs, fmt.Errorf("%s: not an integer: %q", k, v))
		return def
	}
	return n
}

func (g *getter) boolean(k string, def bool) bool {
	v, ok := g.lookup(k)
	if !ok || v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		g.errs = append(g.errs, fmt.Errorf("%s: not a bool: %q", k, v))
		return def
	}
	return b
}

func (g *getter) dur(k string, def time.Duration) time.Duration {
	v, ok := g.lookup(k)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		g.errs = append(g.errs, fmt.Errorf("%s: not a duration: %q", k, v))
		return def
	}
	return d
}
