// Command server is the MotoLink modular monolith (ADR-001). Roles are selected
// by ROLES=api,ws,worker. Subcommand: `server migrate up|down|reset|status`.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/sync/errgroup"

	"motolink/internal/durable"
	"motolink/internal/httpapi"
	"motolink/internal/identity"
	"motolink/internal/platform/config"
	"motolink/internal/platform/httpx"
	"motolink/internal/platform/logx"
	"motolink/internal/platform/metrics"
	"motolink/internal/platform/pg"
	"motolink/internal/platform/ratelimit"
	"motolink/internal/realtime"
	"motolink/internal/tracking"
	"motolink/internal/trip"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.LoadEnv()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := logx.New(os.Stdout, logx.Options{Level: logx.ParseLevel(cfg.LogLevel), AllowCoords: cfg.DebugLogCoords})
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if len(args) >= 1 && args[0] == "migrate" {
		cmd := "up"
		if len(args) >= 2 {
			cmd = args[1]
		}
		return pg.Migrate(ctx, cfg.DatabaseURL, cmd)
	}
	if len(args) >= 1 && args[0] == "healthcheck" {
		return healthcheck(cfg.HTTPAddr)
	}

	pool, err := pg.NewPool(ctx, cfg.DatabaseURL, 20)
	if err != nil {
		return err
	}
	defer pool.Close()

	checks := []httpx.Check{{Name: "postgres", Fn: pool.Ping}}
	var (
		limiter ratelimit.Limiter  = ratelimit.NewMemory()
		revoker identity.Revoker   = identity.NoopRevoker{}
		live    realtime.LiveStore = realtime.NewMemStore()
		bus     realtime.Bus       = realtime.NoBus{}
	)
	if cfg.RedisAddr != "" {
		rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPass})
		defer rdb.Close()
		checks = append(checks, httpx.Check{Name: "redis", Fn: func(c context.Context) error { return rdb.Ping(c).Err() }})
		limiter = ratelimit.Redis{C: rdb}
		revoker = identity.NewRedisRevoker(rdb)
		live = realtime.NewRedisStore(rdb)
		bus = realtime.NewRedisBus(rdb)
	}

	reg := metrics.New()
	g, gctx := errgroup.WithContext(ctx)
	var fanout durable.Fanout = realtime.BusFanout{Bus: bus}

	var (
		sink    realtime.Sink
		writer  httpapi.PointWriter
		kclient *kgo.Client
	)
	needKafka := cfg.HistorySink == "kafka" || cfg.OutboxSink == "kafka"
	if needKafka && (cfg.Roles.API || cfg.Roles.WS || cfg.Roles.Worker) && len(cfg.KafkaBrokers) > 0 {
		kclient, err = durable.NewProducerClient(durable.KafkaConfig{Brokers: cfg.KafkaBrokers, Log: log})
		if err != nil {
			return fmt.Errorf("kafka: %w", err)
		}
		defer kclient.Close()
		checks = append(checks, httpx.Check{Name: "kafka", Warn: true, Fn: kclient.Ping})
	}
	switch {
	case cfg.HistorySink == "direct_pg":
		pgs := durable.NewPGSink(pool, durable.PGSinkOptions{Log: log, Reg: reg.Reg})
		pgs.Start(gctx)
		defer pgs.Wait() // flushes what is queued once gctx ends
		sink, writer = pgs, pgs
	case kclient != nil:
		ks := durable.NewKafkaSink(kclient, log, reg.Reg)
		sink, writer = ks, ks
	}

	root := http.NewServeMux()
	root.Handle("/", httpx.HealthMux(checks...))
	if cfg.Roles.API {
		signer, err := identity.NewSigner(cfg.JWTPrivateKeyFile, cfg.JWTKeyID, cfg.AccessTTL, cfg.Env != "prod")
		if err != nil {
			return fmt.Errorf("jwt signer: %w", err)
		}
		if cfg.JWTPrivateKeyFile == "" {
			log.Warn("using an ephemeral JWT signing key: all tokens become invalid on restart (dev/test only)")
		}
		svc := identity.NewService(identity.Deps{
			DB: pool, Hasher: identity.NewHasher(identity.DefaultArgon(cfg.Argon2Concurrency)), Signer: signer,
			Revoker: revoker, AccessTTL: cfg.AccessTTL, RefreshTTL: cfg.RefreshTTL, Log: log,
		})
		trips := trip.NewService(pool, nil)
		sessions := tracking.NewService(pool, trips, svc, nil)
		a := &httpapi.API{Identity: svc, Trips: trips, Sessions: sessions, DB: pool, Limiter: limiter, Log: log,
			TrustedProxyHops: cfg.TrustedProxyHops, Client: httpapi.DefaultClientConfig(), Registerer: reg.Reg, Points: writer}
		root.Handle("/v1/", a.Handler())

		if cfg.Roles.WS {
			hub := realtime.NewHub(realtime.Options{Log: log, TrustedProxyHops: cfg.TrustedProxyHops}, realtime.Deps{
				Live: live, Bus: bus, Trips: trips, Sessions: sessions,
				Auth: realtime.IdentityAuth{Svc: svc}, Metrics: realtime.NewMetrics(reg.Reg), Sink: sink,
			})
			trips.SetPublisher(hub)
			trips.SetLive(hub)
			sessions.SetPublisher(hub)
			hub.Start(gctx)
			fanout = hub
			root.Handle("GET /v1/ws", hub)
			defer func() {
				sctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
				defer cancel()
				hub.Shutdown(sctx)
			}()
		}
	}

	if cfg.Roles.Worker {
		disp := &durable.Dispatcher{Fanout: fanout, Log: log}
		var pub durable.Publisher = disp.Publisher()
		if cfg.OutboxSink == "kafka" && kclient != nil {
			pub = durable.KafkaPublisher(kclient)
			kc := durable.KafkaConfig{Brokers: cfg.KafkaBrokers, Log: log, Reg: reg.Reg}
			g.Go(func() error { return (&durable.Notifier{D: disp, Cfg: kc}).Run(gctx) })
		}
		if cfg.HistorySink == "kafka" && len(cfg.KafkaBrokers) > 0 {
			hw := &durable.HistoryWriter{DB: pool, Cfg: durable.KafkaConfig{Brokers: cfg.KafkaBrokers, Log: log, Reg: reg.Reg}}
			g.Go(func() error { return hw.Run(gctx) })
		}
		relay := durable.NewRelay(pool, pub, durable.RelayOptions{Log: log, Reg: reg.Reg})
		g.Go(func() error { relay.Run(gctx); return nil })
	}

	api := &http.Server{Addr: cfg.HTTPAddr, Handler: root, ReadHeaderTimeout: 5 * time.Second}
	g.Go(func() error { return httpx.Run(gctx, log, "api", api, cfg.ShutdownTimeout) })

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", reg.Handler())
	ms := &http.Server{Addr: cfg.MetricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	g.Go(func() error { return httpx.Run(gctx, log, "metrics", ms, cfg.ShutdownTimeout) })

	log.Info("motolink server started", "env", cfg.Env, "roles", fmt.Sprintf("%+v", cfg.Roles),
		"history_sink", cfg.HistorySink, "outbox_sink", cfg.OutboxSink)
	return g.Wait()
}

func healthcheck(addr string) error {
	if len(addr) > 0 && addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("healthz status %d", resp.StatusCode)
	}
	return nil
}
