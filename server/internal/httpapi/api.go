package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"motolink/internal/identity"
	"motolink/internal/platform/ratelimit"
	"motolink/internal/sos"
	"motolink/internal/tracking"
	"motolink/internal/trip"
)

// ClientConfig is served by GET /v1/config (docs/design/06 §1.2).
type ClientConfig struct {
	MinAppVersion string `json:"min_app_version"`
	WSVMin        int    `json:"ws_v_min"`
	WSVMax        int    `json:"ws_v_max"`
	TileStyleURL  string `json:"tile_style_url"` // empty until the map spike (M1-T06) decides the source
	Limits        Limits `json:"limits"`
}

type Limits struct {
	MaxPointsPerFrame int `json:"max_points_per_frame"`
	MaxFrameBytes     int `json:"max_frame_bytes"`
	MaxTripMembers    int `json:"max_trip_members"`
}

// DefaultClientConfig matches docs/design/07 §3.1.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{MinAppVersion: "0.0.0", WSVMin: 1, WSVMax: 1,
		Limits: Limits{MaxPointsPerFrame: 20, MaxFrameBytes: 16384, MaxTripMembers: 15}}
}

// API wires the REST handlers.
type API struct {
	Identity         *identity.Service
	Trips            *trip.Service
	Sessions         *tracking.Service
	DB               *pgxpool.Pool
	Limiter          ratelimit.Limiter
	Log              *slog.Logger
	TrustedProxyHops int
	RegisterPerHour  int // 0 = 10
	Client           ClientConfig
	Registerer       prometheus.Registerer // optional
	Telemetry        TelemetrySink         // optional: client quality samples
	SOS              *sos.Service
	Notifications    NotificationStore
	Points           PointWriter // durable point writer for POST .../points (nil → 503)

	m metricSet
}

// Handler builds the router. Call once.
func (a *API) Handler() http.Handler {
	if a.Log == nil {
		a.Log = slog.Default()
	}
	a.m = metricSet{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "http_requests_total", Help: "HTTP requests by route and status class."}, []string{"route", "class"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "http_request_duration_seconds", Help: "HTTP latency by route.", Buckets: prometheus.DefBuckets}, []string{"route"}),
	}
	if a.Registerer != nil {
		a.Registerer.MustRegister(a.m.requests, a.m.duration)
	}

	mux := http.NewServeMux()
	// public
	mux.Handle("GET /v1/config", a.h(a.getConfig))
	mux.Handle("POST /v1/auth/register", a.h(a.register))
	mux.Handle("POST /v1/auth/login", a.h(a.login))
	mux.Handle("POST /v1/auth/refresh", a.h(a.refresh))
	// authenticated
	mux.Handle("POST /v1/auth/logout", a.authed(a.logout))
	mux.Handle("POST /v1/auth/logout-all", a.authed(a.logoutAll))
	mux.Handle("GET /v1/me", a.authed(a.getMe))
	mux.Handle("PATCH /v1/me", a.authed(a.patchMe))
	a.accountRoutes(mux)
	if a.Trips != nil {
		a.tripRoutes(mux)
	}
	if a.Sessions != nil {
		a.sessionRoutes(mux)
	}
	if a.SOS != nil {
		a.sosRoutes(mux)
	}
	if a.Notifications != nil {
		a.notificationRoutes(mux)
	}
	return a.base(mux)
}

// TelemetrySink receives anonymous client-side samples (age of displayed positions, ACK latency).
type TelemetrySink interface {
	Observe(locationAgeS, ackLatencyS []float64)
}
