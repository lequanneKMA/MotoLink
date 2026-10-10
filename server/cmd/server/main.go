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
	"golang.org/x/sync/errgroup"

	"motolink/internal/platform/config"
	"motolink/internal/platform/httpx"
	"motolink/internal/platform/logx"
	"motolink/internal/platform/metrics"
	"motolink/internal/platform/pg"
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
	if cfg.RedisAddr != "" {
		rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPass})
		defer rdb.Close()
		checks = append(checks, httpx.Check{Name: "redis", Fn: func(c context.Context) error { return rdb.Ping(c).Err() }})
	}

	reg := metrics.New()
	g, gctx := errgroup.WithContext(ctx)

	api := &http.Server{Addr: cfg.HTTPAddr, Handler: httpx.HealthMux(checks...), ReadHeaderTimeout: 5 * time.Second}
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
