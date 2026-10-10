// Package logx builds the structured logger. Coordinates must never be logged
// (ADR-016, docs/design/11 §4.5): attributes with location-like keys are redacted
// at the handler, whatever the call site passes.
package logx

import (
	"io"
	"log/slog"
	"strings"
)

const redacted = "[REDACTED]"

var sensitiveKeys = map[string]struct{}{
	"lat": {}, "lng": {}, "lon": {}, "latitude": {}, "longitude": {},
	"coords": {}, "coordinates": {}, "points": {}, "route": {}, "location": {},
	"polyline": {}, "route_polyline": {},
}

// IsSensitiveKey reports whether a log attribute key must be redacted.
func IsSensitiveKey(k string) bool {
	_, ok := sensitiveKeys[strings.ToLower(k)]
	return ok
}

// Options configure New.
type Options struct {
	Level slog.Level
	// AllowCoords disables redaction. Only for explicit local debugging;
	// config validation forbids it in production.
	AllowCoords bool
}

// New returns a JSON slog.Logger writing to w with coordinate redaction.
func New(w io.Writer, o Options) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: o.Level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if o.AllowCoords {
				return a
			}
			if IsSensitiveKey(a.Key) {
				return slog.String(a.Key, redacted)
			}
			return a
		},
	})
	return slog.New(h)
}

// ParseLevel maps a text level to slog.Level (default info).
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
