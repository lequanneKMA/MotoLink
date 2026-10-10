package logx

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactsCoordinateKeys(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, Options{Level: slog.LevelDebug})
	l.Info("update", "lat", 11.123456, "lng", 108.654321, "user_id", "u1",
		slog.Group("loc", slog.Float64("latitude", 11.123456)),
		"points", []float64{11.123456, 108.654321})
	out := buf.String()
	for _, secret := range []string{"11.123456", "108.654321"} {
		if strings.Contains(out, secret) {
			t.Fatalf("log leaked coordinate %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, `"user_id":"u1"`) {
		t.Fatalf("non-sensitive field lost: %s", out)
	}
}

func TestAllowCoordsDisablesRedaction(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, Options{AllowCoords: true})
	l.Info("x", "lat", 1.5)
	if !strings.Contains(buf.String(), "1.5") {
		t.Fatalf("expected coordinate when AllowCoords: %s", buf.String())
	}
}
