package httpapi

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"motolink/internal/platform/id"
	"motolink/internal/trip"
)

func TestSOSFlow(t *testing.T) {
	e := newEnv(t)
	owner, a, b, outsider := e.newUser(t, "o"), e.newUser(t, "a"), e.newUser(t, "b"), e.newUser(t, "x")
	tid := e.newTrip(t, owner, false)
	code := e.invite(t, owner, tid, nil)
	e.join(t, a, code)
	e.join(t, b, code)

	cid := id.NewString()
	body := map[string]any{"client_sos_id": cid, "kind": "breakdown", "message": "Hỏng xe", "lat": 11.94, "lng": 108.45, "accuracy_m": 12}
	if c, m := e.call(t, a, "POST", "/v1/trips/"+tid+"/sos", body); c != 409 || m["code"] != "trip_not_active" {
		t.Fatalf("draft trip: %d %v", c, m)
	}
	e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/start", nil, 200)

	m := e.mustCall(t, a, "POST", "/v1/trips/"+tid+"/sos", body, 201)
	sid := m["sos_id"].(string)
	seq := m["event_seq"].(float64)
	if seq == 0 || m["duplicate"] != false {
		t.Fatalf("%v", m)
	}
	// retry with the same client_sos_id: 200 duplicate, same ids, no second event
	m2 := e.mustCall(t, a, "POST", "/v1/trips/"+tid+"/sos", body, 200)
	if m2["sos_id"] != sid || m2["event_seq"] != seq || m2["duplicate"] != true {
		t.Fatalf("%v", m2)
	}
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM trip_events WHERE trip_id=$1 AND type='sos_created'`, tid).Scan(&n)
	if n != 1 {
		t.Fatalf("sos events=%d", n)
	}
	// validation / authorization
	if c, _ := e.call(t, a, "POST", "/v1/trips/"+tid+"/sos", map[string]any{"client_sos_id": id.NewString(), "kind": "nope"}); c != 400 {
		t.Fatalf("bad kind: %d", c)
	}
	if c, _ := e.call(t, outsider, "POST", "/v1/trips/"+tid+"/sos", map[string]any{"client_sos_id": id.NewString()}); c != 404 {
		t.Fatalf("outsider: %d", c)
	}
	if c, _ := e.call(t, outsider, "GET", "/v1/sos/"+sid, nil); c != 404 {
		t.Fatalf("outsider get: %d", c)
	}

	// receipts are recipient-only and update the aggregate the sender sees
	if c, _ := e.call(t, a, "POST", "/v1/sos/"+sid+"/receipt", map[string]any{"status": "received"}); c != 404 {
		t.Fatalf("sender is not a recipient: %d", c)
	}
	e.mustCall(t, b, "POST", "/v1/sos/"+sid+"/receipt", map[string]any{"status": "received"}, 204)
	e.mustCall(t, b, "POST", "/v1/sos/"+sid+"/receipt", map[string]any{"status": "seen"}, 204)
	e.mustCall(t, b, "POST", "/v1/sos/"+sid+"/receipt", map[string]any{"status": "received"}, 204) // idempotent, never downgrades
	got := e.mustCall(t, a, "GET", "/v1/sos/"+sid, nil, 200)["sos"].(map[string]any)
	d := got["delivery"].(map[string]any)
	if d["total"].(float64) != 2 || d["received"].(float64) != 1 || d["seen"].(float64) != 1 {
		t.Fatalf("delivery %v", d)
	}
	if len(got["recipients"].([]any)) != 2 {
		t.Fatalf("sender sees per-recipient detail: %v", got["recipients"])
	}
	if _, ok := e.mustCall(t, b, "GET", "/v1/sos/"+sid, nil, 200)["sos"].(map[string]any)["recipients"]; ok {
		t.Fatal("recipients leaked to a non-sender")
	}
	list := e.mustCall(t, b, "GET", "/v1/trips/"+tid+"/sos?status=open", nil, 200)["items"].([]any)
	if len(list) != 1 {
		t.Fatalf("list %v", list)
	}

	// resolve: sender or owner only
	if c, _ := e.call(t, b, "POST", "/v1/sos/"+sid+"/resolve", map[string]any{"status": "resolved"}); c != 403 {
		t.Fatalf("other member: %d", c)
	}
	r := e.mustCall(t, owner, "POST", "/v1/sos/"+sid+"/resolve", map[string]any{"status": "resolved"}, 200)["sos"].(map[string]any)
	if r["status"] != "resolved" {
		t.Fatalf("%v", r)
	}
	if c, _ := e.call(t, a, "POST", "/v1/sos/"+sid+"/resolve", map[string]any{"status": "cancelled"}); c != 409 {
		t.Fatalf("second resolve: %d", c)
	}

	// notifications: derived from the durable events, idempotent per event, readable and markable
	ctx := context.Background()
	tripUUID := uuid.MustParse(tid)
	evs, err := trip.ListEvents(ctx, e.pool, tripUUID, 0, 100, time_zero())
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ { // second pass = replay of the same events
		for _, ev := range evs {
			if err := e.notif.OnEvent(ctx, ev); err != nil {
				t.Fatal(err)
			}
		}
	}
	items := e.mustCall(t, b, "GET", "/v1/notifications", nil, 200)["items"].([]any)
	types := map[string]int{}
	for _, it := range items {
		types[it.(map[string]any)["type"].(string)]++
	}
	if types["sos"] != 1 || types["sos_resolved"] != 1 || types["trip_started"] != 1 {
		t.Fatalf("notification types after replay: %v", types)
	}
	if c, _ := e.call(t, a, "GET", "/v1/notifications", nil); c != 200 {
		t.Fatal(c)
	}
	e.mustCall(t, b, "POST", "/v1/notifications/read", map[string]any{"all": true}, 204)
	for _, it := range e.mustCall(t, b, "GET", "/v1/notifications", nil, 200)["items"].([]any) {
		if it.(map[string]any)["read_at"] == nil {
			t.Fatal("not marked read")
		}
	}
	// the sender is never notified of their own SOS
	for _, it := range e.mustCall(t, a, "GET", "/v1/notifications", nil, 200)["items"].([]any) {
		if it.(map[string]any)["type"] == "sos" {
			t.Fatal("sender got own SOS notification")
		}
	}
	// recipients' push status without FCM => no_token
	var st string
	_ = e.pool.QueryRow(ctx, `SELECT push_status FROM sos_recipients WHERE sos_id=$1 AND user_id=$2`, sid, b.id).Scan(&st)
	if st != "no_token" {
		t.Fatalf("push_status=%q", st)
	}
}
