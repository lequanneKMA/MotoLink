package httpapi

import (
	"context"
	"testing"
	"time"

	"motolink/internal/durable"
)

func TestRESTPointsBackfillIsIdempotentAndValidated(t *testing.T) {
	e := newEnv(t)
	e.api.Points = durable.NewPGSink(e.pool, durable.PGSinkOptions{})
	owner, other := e.newUser(t, "o"), e.newUser(t, "x")
	tid := e.newTrip(t, owner, true)
	e.consent(t, owner)
	e.consent(t, other)
	sid := e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/tracking-sessions", map[string]any{}, 201)["session"].(map[string]any)["id"].(string)

	now := time.Now().UnixMilli()
	pt := func(seq int, lat float64, acc float64) map[string]any {
		return map[string]any{"seq": seq, "client_ts": now, "lat": lat, "lng": 106.7, "accuracy_m": acc}
	}
	body := map[string]any{"points": []any{pt(1, 10.77, 8), pt(2, 10.78, 9), pt(3, 99, 8), pt(4, 10.79, 500)}}
	m := e.mustCall(t, owner, "POST", "/v1/tracking-sessions/"+sid+"/points", body, 200)
	if m["up_to_seq"].(float64) != 4 {
		t.Fatalf("up_to_seq %v", m["up_to_seq"])
	}
	rej := m["rejected"].([]any)
	if len(rej) != 2 || rej[0].(map[string]any)["reason"] != "invalid_coordinates" || rej[1].(map[string]any)["reason"] != "low_accuracy" {
		t.Fatalf("rejected %v", rej)
	}
	e.mustCall(t, owner, "POST", "/v1/tracking-sessions/"+sid+"/points", body, 200) // resend: no error, no duplicates
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM location_points WHERE session_id=$1`, sid).Scan(&n); err != nil || n != 2 {
		t.Fatalf("rows=%d err=%v want 2", n, err)
	}
	var last int
	_ = e.pool.QueryRow(context.Background(), `SELECT last_seq FROM tracking_sessions WHERE id=$1`, sid).Scan(&last)
	if last != 4 {
		t.Fatalf("last_seq=%d", last)
	}
	if c, _ := e.call(t, other, "POST", "/v1/tracking-sessions/"+sid+"/points", body); c != 404 {
		t.Fatalf("other user's session => 404, got %d", c)
	}
	if c, _ := e.call(t, owner, "POST", "/v1/tracking-sessions/"+sid+"/points", map[string]any{"points": []any{pt(5, 10, 8), pt(5, 10, 8)}}); c != 400 {
		t.Fatalf("non-increasing seq => 400, got %d", c)
	}
	e.mustCall(t, owner, "POST", "/v1/tracking-sessions/"+sid+"/end", nil, 200)
	if c, m := e.call(t, owner, "POST", "/v1/tracking-sessions/"+sid+"/points", body); c != 409 || m["code"] != "session_not_open" {
		t.Fatalf("%d %v", c, m)
	}
}
