package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"motolink/internal/identity"
	"motolink/internal/platform/apperr"
	"motolink/internal/platform/id"
)

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyPrincipal
	keyReqInfo
)

// reqInfo lets inner middleware report data (the caller) back to the outer access log.
type reqInfo struct{ user uuid.UUID }

// Principal is the authenticated caller. Authorisation for trip resources is
// never derived from it (docs/design/11 §3): handlers check membership per request.
type Principal struct {
	UserID uuid.UUID
	SID    uuid.UUID
}

// RequestID returns the request id stored by the middleware.
func RequestID(ctx context.Context) string { s, _ := ctx.Value(keyRequestID).(string); return s }

// PrincipalFrom returns the caller set by the auth middleware.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(keyPrincipal).(Principal)
	return p, ok
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(c int) {
	if !w.wrote {
		w.status, w.wrote = c, true
	}
	w.ResponseWriter.WriteHeader(c)
}
func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status, w.wrote = http.StatusOK, true
	}
	return w.ResponseWriter.Write(b)
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// base wraps every request: request id, panic recovery, access log (never bodies), metrics.
func (a *API) base(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rid := id.NewString()
		w := &statusWriter{ResponseWriter: rw, status: http.StatusOK}
		w.Header().Set("X-Request-Id", rid)
		info := &reqInfo{}
		ctx := context.WithValue(context.WithValue(r.Context(), keyRequestID, rid), keyReqInfo, info)
		r = r.WithContext(ctx)
		defer func() {
			if rec := recover(); rec != nil {
				a.Log.Error("panic in handler", "request_id", rid, "panic", rec, "stack", string(debug.Stack()))
				if !w.wrote {
					writeError(w, r, apperr.Internal(errors.New("panic")))
				}
			}
			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			a.m.requests.WithLabelValues(route, statusClass(w.status)).Inc()
			a.m.duration.WithLabelValues(route).Observe(time.Since(start).Seconds())
			lvl := slog.LevelInfo
			if w.status >= 500 {
				lvl = slog.LevelError
			}
			attrs := []any{"request_id", rid, "method", r.Method, "endpoint", route, "status", w.status,
				"dur_ms", time.Since(start).Milliseconds()}
			if info.user != uuid.Nil {
				attrs = append(attrs, "user_id", info.user.String())
			}
			a.Log.Log(r.Context(), lvl, "http", attrs...)
		}()
		next.ServeHTTP(w, r)
	})
}

func statusClass(c int) string {
	switch {
	case c >= 500:
		return "5xx"
	case c >= 400:
		return "4xx"
	case c >= 300:
		return "3xx"
	default:
		return "2xx"
	}
}

type handlerFunc func(w http.ResponseWriter, r *http.Request) error
type authedFunc func(w http.ResponseWriter, r *http.Request, p Principal) error

// h adapts an error-returning handler.
func (a *API) h(fn handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			a.fail(w, r, err)
		}
	})
}

func (a *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	if e, ok := apperr.As(err); !ok || e.Status >= 500 {
		a.Log.Error("request failed", "request_id", RequestID(r.Context()), "err", err)
	}
	writeError(w, r, err)
}

// authed requires a valid access token for a live session.
func (a *API) authed(fn authedFunc) http.Handler {
	return a.h(func(w http.ResponseWriter, r *http.Request) error {
		p, err := a.authenticate(r)
		if err != nil {
			return err
		}
		if info, ok := r.Context().Value(keyReqInfo).(*reqInfo); ok {
			info.user = p.UserID
		}
		r = r.WithContext(context.WithValue(r.Context(), keyPrincipal, p))
		return fn(w, r, p)
	})
}

func (a *API) authenticate(r *http.Request) (Principal, error) {
	h := r.Header.Get("Authorization")
	const pfx = "Bearer "
	if len(h) <= len(pfx) || !strings.EqualFold(h[:len(pfx)], pfx) {
		return Principal{}, apperr.Unauthorized()
	}
	claims, err := a.Identity.Signer().Verify(strings.TrimSpace(h[len(pfx):]))
	switch {
	case errors.Is(err, identity.ErrTokenExpired):
		return Principal{}, apperr.TokenExpired()
	case err != nil:
		return Principal{}, apperr.Unauthorized()
	}
	uid, _ := claims.UserID()
	sid, _ := uuid.Parse(claims.SID)
	active, err := a.Identity.SessionActive(r.Context(), sid)
	if err != nil {
		return Principal{}, apperr.Internal(err)
	}
	if !active {
		return Principal{}, apperr.SessionRevoked()
	}
	return Principal{UserID: uid, SID: sid}, nil
}

// clientIP returns the caller IP. X-Forwarded-For is trusted only for the
// configured number of proxy hops (Render/Caddy in front); otherwise RemoteAddr.
func (a *API) clientIP(r *http.Request) string {
	if a.TrustedProxyHops > 0 {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			i := len(parts) - a.TrustedProxyHops
			if i < 0 {
				i = 0
			}
			if ip := strings.TrimSpace(parts[i]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limit enforces a fixed-window rate limit. Limiter failures fail open (logged):
// availability of login beats strictness when Redis is down [GIẢ ĐỊNH, docs/design/08 §5].
func (a *API) limit(ctx context.Context, key string, max int64, window time.Duration) error {
	if a.Limiter == nil {
		return nil
	}
	n, retry, err := a.Limiter.Hit(ctx, key, window)
	if err != nil {
		a.Log.Warn("rate limiter unavailable; failing open", "err", err)
		return nil
	}
	if n > max {
		return apperr.RateLimited(retry)
	}
	return nil
}

type metricSet struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}
