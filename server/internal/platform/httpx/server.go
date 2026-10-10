// Package httpx holds small HTTP helpers: health endpoints and graceful server run.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Check is a readiness probe.
type Check struct {
	Name string
	Fn   func(context.Context) error
	Warn bool // a failure is reported but does not fail readiness (e.g. Kafka, docs/design/06 §2.9)
}

// HealthMux returns /healthz (liveness) and /readyz (all checks must pass).
func HealthMux(checks ...Check) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		res := map[string]string{}
		code := http.StatusOK
		for _, c := range checks {
			if err := c.Fn(ctx); err != nil {
				res[c.Name] = err.Error()
				if !c.Warn {
					code = http.StatusServiceUnavailable
				}
			} else {
				res[c.Name] = "ok"
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(res)
	})
	return mux
}

// Run serves srv until ctx is cancelled, then shuts down within timeout.
// It returns nil on a clean shutdown.
func Run(ctx context.Context, log *slog.Logger, name string, srv *http.Server, timeout time.Duration) error {
	errc := make(chan error, 1)
	go func() {
		log.Info("http listening", "server", name, "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	log.Info("http shutting down", "server", name)
	if err := srv.Shutdown(sctx); err != nil {
		return err
	}
	return <-errc
}
