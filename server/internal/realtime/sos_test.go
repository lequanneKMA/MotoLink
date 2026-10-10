package realtime

import (
	"encoding/json"
	"testing"
	"time"

	"motolink/internal/platform/id"
	"motolink/internal/sos"
)

func TestSOSOverWebSocket(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	s := sos.NewService(w.pool, w.trips, n.hub)
	s.SetStatusPusher(n.hub)
	n.hub.SetSOS(sos.WSHandler{S: s})

	a, b, c := w.person("a"), w.person("b"), w.person("c")
	tid := w.tripWith(a, b, c)
	ca, cb, cc := n.dial(t, a), n.dial(t, b), n.dial(t, c)
	for _, x := range []*wsClient{ca, cb, cc} {
		jp := x.join(tid, 0)
		for i := 0; i < jp.Replay.Count; i++ {
			x.expect("trip.event")
		}
	}

	cid := id.NewString()
	ca.send("sos.create", cid, tid.String(), map[string]any{"client_sos_id": cid, "kind": "breakdown", "message": "Hỏng xe", "lat": 11.94, "lng": 108.45})
	ack := decode[AckPayload](t, ca.expect("ack"))
	if !ack.OK || ack.Ref != cid {
		t.Fatalf("%+v", ack)
	}
	// everyone but the sender receives the durable sos.event
	for _, x := range []*wsClient{cb, cc} {
		ev := x.expect("sos.event")
		if ev.EventSeq == 0 {
			t.Fatalf("sos.event without event_seq: %+v", ev)
		}
	}
	ca.expectNone("sos.event", 300*time.Millisecond)

	// resending the same SOS is a duplicate and produces no second event
	ca.send("sos.create", cid, tid.String(), map[string]any{"client_sos_id": cid, "kind": "breakdown"})
	ack2 := decode[AckPayload](t, ca.expect("ack"))
	if !ack2.OK {
		t.Fatalf("%+v", ack2)
	}
	cb.expectNone("sos.event", 300*time.Millisecond)

	// a receipt from b updates the aggregate that only the sender gets
	var sosID string
	_ = w.pool.QueryRow(t.Context(), `SELECT id::text FROM sos_events LIMIT 1`).Scan(&sosID)
	cb.send("sos.receipt", "r1", tid.String(), map[string]any{"sos_id": sosID, "status": "seen"})
	cb.expect("ack")
	st := ca.expect("sos.status")
	var p struct{ Total, Received, Seen int }
	if err := json.Unmarshal(st.Payload, &p); err != nil || p.Total != 2 || p.Seen != 1 || p.Received != 1 {
		t.Fatalf("%s %v", st.Payload, err)
	}
	cc.expectNone("sos.status", 300*time.Millisecond)

	// a stranger's receipt is refused
	out := w.person("x")
	co := n.dial(t, out)
	co.send("sos.receipt", "r2", tid.String(), map[string]any{"sos_id": sosID, "status": "seen"})
	if e := decode[ErrorPayload](t, co.expect("error")); e.Code != "forbidden" {
		t.Fatalf("%+v", e)
	}
}
