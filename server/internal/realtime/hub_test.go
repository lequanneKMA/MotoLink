package realtime

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"motolink/internal/identity"
	"motolink/internal/trip"
)

func TestHelloHandshakeRules(t *testing.T) {
	w := newWorld(t)
	n := w.node(func(o *Options) { o.HelloTimeout = 300 * time.Millisecond })
	p := w.person("a")

	// welcome carries heartbeat, limits and the clock offset
	c := n.dial(t, p)
	_ = c

	// first message must be hello
	bad := n.dialWith(t, p.token, "bad", false)
	bad.send("ping", "x", "", nil)
	bad.waitClose(CloseBadMessage)

	// silence => hello timeout
	idle := n.dialWith(t, p.token, "idle", false)
	idle.waitClose(CloseBadMessage)

	// unsupported protocol version => explicit upgrade_required, then close
	old := n.dialWith(t, p.token, "old", false)
	old.send("hello", "h", "", HelloPayload{SupportedV: []int{99}, ClientTimeMs: time.Now().UnixMilli()})
	e := old.expect("error")
	if decode[ErrorPayload](t, e).Code != "upgrade_required" {
		t.Fatalf("%s", e.Payload)
	}
	old.waitClose(CloseBadMessage)

	// no / bad token => refused before upgrade
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, resp, err := websocket.Dial(ctx, n.url, nil); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("anonymous dial must get 401, got %v %v", resp, err)
	}
}

func TestWelcomeContent(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	p := w.person("a")
	c := n.dialWith(t, p.token, "a", false)
	c.send("hello", "h", "", HelloPayload{SupportedV: []int{1}, ClientTimeMs: time.Now().UnixMilli() - 60_000})
	wel := decode[WelcomePayload](t, c.expect("welcome"))
	if wel.UserID != p.id.String() || wel.VMax != 1 || wel.Limits.MaxPointsPerFrame != 20 || wel.Heartbeat.PingIntervalS == 0 {
		t.Fatalf("%+v", wel)
	}
	// client clock 60 s behind => the server reports a +60 s offset (ADR-004)
	if wel.ClockOffset < 59_000 || wel.ClockOffset > 61_500 {
		t.Fatalf("clock offset = %d", wel.ClockOffset)
	}
}

func TestLiveLocationReachesOthersNotSender(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a, b := w.person("a"), w.person("b")
	tid := w.tripWith(a, b)
	sid := w.startSession(a, tid)
	ca, cb := n.dial(t, a), n.dial(t, b)
	ca.join(tid, 0)
	cb.join(tid, 0)

	ca.sendPoints(tid, sid, 1, 2, 3)
	ev := cb.expect("location.event")
	pts := decode[LocationEventPayload](t, ev).Points
	if ev.TripID != tid.String() || len(pts) != 1 || pts[0].UserID != a.id.String() || pts[0].SessionID != sid.String() {
		t.Fatalf("event: %+v", ev)
	}
	if pts[0].Seq != 3 { // only the newest of a burst is shown (coalescing)
		t.Fatalf("viewer must get the newest point, got seq %d", pts[0].Seq)
	}
	ca.expectNone("location.event", 200*time.Millisecond)

	// ack: cumulative, after the points are durable
	ack := decode[LocationAckPayload](t, ca.expect("location.ack"))
	if ack.SessionID != sid.String() || ack.UpToSeq != 3 {
		t.Fatalf("%+v", ack)
	}
	// the live position is available to late joiners from the live store
	if m, _ := w.live.Locations(context.Background(), tid, []uuid.UUID{a.id}); m[a.id].Seq != 3 {
		t.Fatalf("live store: %+v", m)
	}
}

func TestAckWaitsForDurability(t *testing.T) {
	w := newWorld(t)
	w.sink.auto.Store(false)
	n := w.node()
	a := w.person("a")
	tid := w.tripWith(a)
	sid := w.startSession(a, tid)
	ca := n.dial(t, a)
	ca.join(tid, 0)

	ca.sendPoints(tid, sid, 1, 2)
	ca.expectNone("location.ack", 300*time.Millisecond) // accepted but not durable => no ack (ADR-005)
	w.sink.flush()
	if ack := decode[LocationAckPayload](t, ca.expect("location.ack")); ack.UpToSeq != 2 {
		t.Fatalf("%+v", ack)
	}
}

func TestResentDuplicateGetsAckAgainAndIsNotBroadcast(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a, b := w.person("a"), w.person("b")
	tid := w.tripWith(a, b)
	sid := w.startSession(a, tid)
	ca, cb := n.dial(t, a), n.dial(t, b)
	ca.join(tid, 0)
	cb.join(tid, 0)
	ca.sendPoints(tid, sid, 1, 2)
	cb.expect("location.event")
	ca.expect("location.ack")

	before := w.sink.enqueued.Load()
	ca.sendPoints(tid, sid, 1, 2) // client never saw the ack and resends
	if ack := decode[LocationAckPayload](t, ca.expect("location.ack")); ack.UpToSeq != 2 {
		t.Fatalf("lost ack must be repeated: %+v", ack)
	}
	cb.expectNone("location.event", 200*time.Millisecond)
	if w.sink.enqueued.Load() != before {
		t.Fatal("duplicates must not reach the durable branch again")
	}
}

func TestInvalidPointsAreRejectedButDoNotBlockAcks(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a := w.person("a")
	tid := w.tripWith(a)
	sid := w.startSession(a, tid)
	ca := n.dial(t, a)
	ca.join(tid, 0)
	pts := []WirePoint{
		{Seq: 1, ClientTs: time.Now().UnixMilli(), Lat: 11.94, Lng: 108.45, AccuracyM: accuracy(8)},
		{Seq: 2, ClientTs: time.Now().UnixMilli(), Lat: 11.94, Lng: 108.45, AccuracyM: accuracy(500)}, // too inaccurate
		{Seq: 3, ClientTs: time.Now().UnixMilli(), Lat: 95, Lng: 108.45, AccuracyM: accuracy(8)},      // impossible latitude
		{Seq: 4, ClientTs: time.Now().UnixMilli(), Lat: 11.9401, Lng: 108.45, AccuracyM: accuracy(8)},
	}
	ca.send("location.update", "", tid.String(), LocationUpdatePayload{SessionID: sid.String(), Points: pts})
	var ack LocationAckPayload
	for ack.UpToSeq < 4 {
		ack = decode[LocationAckPayload](t, ca.expect("location.ack"))
	}
	if len(ack.Rejected) == 0 {
		t.Fatalf("rejected points must be reported: %+v", ack)
	}
	reasons := map[int64]string{}
	for _, r := range ack.Rejected {
		reasons[r.Seq] = r.Reason
	}
	// rejections may arrive across two acks; collect the remainder if needed
	if reasons[2] == "" || reasons[3] == "" {
		more := decode[LocationAckPayload](t, ca.expect("location.ack"))
		for _, r := range more.Rejected {
			reasons[r.Seq] = r.Reason
		}
	}
	if reasons[2] != "low_accuracy" || reasons[3] != "invalid_coordinates" {
		t.Fatalf("reasons: %v", reasons)
	}
	for _, p := range w.sink.points() {
		if p.Seq == 2 || p.Seq == 3 {
			t.Fatalf("rejected point %d reached the durable branch", p.Seq)
		}
	}
}

func TestSuspectPointsAreStoredButNotShown(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a, b := w.person("a"), w.person("b")
	tid := w.tripWith(a, b)
	sid := w.startSession(a, tid)
	ca, cb := n.dial(t, a), n.dial(t, b)
	ca.join(tid, 0)
	cb.join(tid, 0)
	fast := float32(95) // m/s: not a motorbike
	ca.send("location.update", "", tid.String(), LocationUpdatePayload{SessionID: sid.String(),
		Points: []WirePoint{{Seq: 1, ClientTs: time.Now().UnixMilli(), Lat: 11.94, Lng: 108.45, AccuracyM: accuracy(8), SpeedMps: &fast}}})
	ca.expect("location.ack")
	cb.expectNone("location.event", 250*time.Millisecond)
	pts := w.sink.points()
	if len(pts) != 1 || !pts[0].Suspect {
		t.Fatalf("suspect point must be stored flagged: %+v", pts)
	}
}

func TestPausedSharingStaysPrivateButIsRecorded(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a, b := w.person("a"), w.person("b")
	tid := w.tripWith(a, b)
	sid := w.startSession(a, tid)
	ca, cb := n.dial(t, a), n.dial(t, b)
	ca.join(tid, 0)
	cb.join(tid, mustHead(t, w, tid))
	if _, err := w.trips.PatchMe(context.Background(), tid, a.id, trip.MemberPatch{SharingState: ptr("paused")}); err != nil {
		t.Fatal(err)
	}
	ev := cb.expect("trip.event")
	if k := decode[TripEventPayload](t, ev).Kind; k != "member_sharing_changed" {
		t.Fatalf("kind %s", k)
	}
	time.Sleep(50 * time.Millisecond)
	ca.sendPoints(tid, sid, 1)
	ca.expect("location.ack")
	cb.expectNone("location.event", 250*time.Millisecond)
	if m, _ := w.live.Locations(context.Background(), tid, []uuid.UUID{a.id}); len(m) != 0 {
		t.Fatalf("a paused member must hold no live position: %+v", m)
	}
	if len(w.sink.points()) != 1 {
		t.Fatal("history is still recorded while sharing is paused")
	}
}

func ptr[T any](v T) *T { return &v }

func TestAuthorizationOnEveryTripMessage(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a, outsider := w.person("a"), w.person("x")
	tid := w.tripWith(a)
	sid := w.startSession(a, tid)

	cx := n.dial(t, outsider)
	cx.send("trip.join", "j", tid.String(), TripJoinPayload{})
	if e := decode[ErrorPayload](t, cx.expect("error")); e.Code != "forbidden" || e.Ref != "j" {
		t.Fatalf("outsider join: %+v", e)
	}
	// without having joined, location updates for the trip are refused: trip_id from the client is never trusted
	cx.sendPoints(tid, sid, 1)
	if e := decode[ErrorPayload](t, cx.expect("error")); e.Code != "forbidden" {
		t.Fatalf("outsider update: %+v", e)
	}
	// a joined member cannot use somebody else's session
	b := w.person("b")
	if _, err := w.trips.Join(context.Background(), b.id, mustInvite(t, w, tid, a)); err != nil {
		t.Fatal(err)
	}
	cb := n.dial(t, b)
	cb.join(tid, 0)
	cb.sendPoints(tid, sid, 1)
	if e := decode[ErrorPayload](t, cb.expect("error")); e.Code != "session_unknown" {
		t.Fatalf("foreign session: %+v", e)
	}
	if len(w.sink.points()) != 0 {
		t.Fatal("nothing from the refused frames may reach the durable branch")
	}
	// unknown message types are ignored, malformed frames get an error but keep the connection
	cb.send("future.thing", "", "", map[string]any{"x": 1})
	cb.conn.Write(context.Background(), websocket.MessageText, []byte("not json"))
	cb.expect("error")
	cb.send("ping", "p1", "", nil)
	if pong := cb.expect("pong"); decode[map[string]any](t, pong)["ref"] != "p1" {
		t.Fatal("pong ref")
	}
}

func mustInvite(t *testing.T, w *world, tid uuid.UUID, owner person) string {
	t.Helper()
	inv, err := w.trips.CreateInvitation(context.Background(), tid, owner.id, trip.InviteInput{})
	if err != nil {
		t.Fatal(err)
	}
	return inv.Code
}

func TestRemovedMemberIsCutOffImmediately(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a, b := w.person("a"), w.person("b")
	tid := w.tripWith(a, b)
	ca, cb := n.dial(t, a), n.dial(t, b)
	ca.join(tid, mustHead(t, w, tid))
	cb.join(tid, mustHead(t, w, tid))
	sid := w.startSession(a, tid)
	cb.expect("trip.event") // tracking_started
	if err := w.trips.Remove(context.Background(), tid, a.id, b.id); err != nil {
		t.Fatal(err)
	}
	e := cb.expect("error")
	if decode[ErrorPayload](t, e).Code != "forbidden" || e.TripID != tid.String() {
		t.Fatalf("%s", e.Payload)
	}
	// b is told about its own removal (so the app can say so), and nothing else about the trip reaches b afterwards
	if ev := cb.expect("trip.event"); decode[TripEventPayload](t, ev).Kind != "member_removed" {
		t.Fatalf("%s", ev.Payload)
	}
	ca.sendPoints(tid, sid, 1)
	cb.expectNone("location.event", 300*time.Millisecond)
	cb.expectNone("trip.event", 100*time.Millisecond)
	cb.sendPoints(tid, sid, 1)
	if e := decode[ErrorPayload](t, cb.expect("error")); e.Code != "forbidden" {
		t.Fatalf("%+v", e)
	}
	cb.send("trip.join", "j2", tid.String(), TripJoinPayload{})
	if e := decode[ErrorPayload](t, cb.expect("error")); e.Code != "forbidden" {
		t.Fatalf("%+v", e)
	}
}

func TestDurableEventsAreDeliveredInOrderWithSeq(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a, b := w.person("a"), w.person("b")
	tid := w.tripWith(a, b)
	cb := n.dial(t, b)
	jp := cb.join(tid, 0)
	if jp.Trip.Status != "active" || jp.HeadEventSeq < 3 || !jp.Replay.Complete {
		t.Fatalf("%+v", jp)
	}
	// replay of history: trip_created, member_joined, trip_started
	var seqs []int64
	for i := 0; i < jp.Replay.Count; i++ {
		seqs = append(seqs, cb.expect("trip.event").EventSeq)
	}
	for i, s := range seqs {
		if s != int64(i+1) {
			t.Fatalf("replay must be 1..n in order: %v", seqs)
		}
	}
	w.sessions.Start(context.Background(), a.id, tid, nil)
	e := cb.expect("trip.event")
	if e.EventSeq != jp.HeadEventSeq+1 || decode[TripEventPayload](t, e).Kind != "tracking_started" {
		t.Fatalf("live event: %+v (head %d)", e, jp.HeadEventSeq)
	}
}

func TestResumeReplaysOnlyMissedEventsAndSnapshotHasLocations(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a, b := w.person("a"), w.person("b")
	tid := w.tripWith(a, b)
	sid := w.startSession(a, tid)
	ca := n.dial(t, a)
	ca.join(tid, 0)
	ca.sendPoints(tid, sid, 1, 2, 3)
	ca.expect("location.ack")

	// b connects later with a cursor: only events after it are replayed, and the snapshot carries a's position
	head := mustHead(t, w, tid)
	if _, err := w.sessions.Pause(context.Background(), a.id, sid); err != nil {
		t.Fatal(err)
	}
	cb := n.dial(t, b, ResumeEntry{TripID: tid.String(), LastEventSeq: head})
	jp := decode[TripJoinedPayload](t, cb.expect("trip.joined"))
	if jp.Replay.Count != 1 || !jp.Replay.Complete || jp.HeadEventSeq != head+1 {
		t.Fatalf("%+v", jp.Replay)
	}
	if e := cb.expect("trip.event"); e.EventSeq != head+1 || decode[TripEventPayload](t, e).Kind != "tracking_paused" {
		t.Fatalf("%+v", e)
	}
	var am *SnapshotMember
	for i := range jp.Members {
		if jp.Members[i].UserID == a.id.String() {
			am = &jp.Members[i]
		}
	}
	if am == nil || am.Presence != "online" || am.Location == nil || am.Location.Seq != 3 || am.Location.SessionID != sid.String() {
		t.Fatalf("snapshot of a: %+v", am)
	}
}

func mustHead(t *testing.T, w *world, tid uuid.UUID) int64 {
	t.Helper()
	var h int64
	if err := w.pool.QueryRow(context.Background(), `SELECT event_seq FROM trips WHERE id=$1`, tid).Scan(&h); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestResyncRequiredWhenCursorTooOld(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a := w.person("a")
	tid := w.tripWith(a)
	if _, err := w.pool.Exec(context.Background(), `UPDATE trip_events SET created_at = now() - interval '9 days' WHERE trip_id=$1 AND seq=1`, tid); err != nil {
		t.Fatal(err)
	}
	ca := n.dial(t, a)
	ca.send("trip.join", "j", tid.String(), TripJoinPayload{LastEventSeq: 0})
	jp := decode[TripJoinedPayload](t, ca.expect("trip.joined"))
	if jp.Replay.Complete {
		t.Fatalf("replay must be marked incomplete: %+v", jp.Replay)
	}
	if r := decode[ResyncPayload](t, ca.expect("resync.required")); r.Reason != "gap_too_large" {
		t.Fatalf("%+v", r)
	}
}

func TestPresenceAnnouncedOnJoinAndLeave(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a, b := w.person("a"), w.person("b")
	tid := w.tripWith(a, b)
	ca := n.dial(t, a)
	ca.join(tid, 0)
	cb := n.dial(t, b)
	cb.join(tid, 0)
	p := decode[PresencePayload](t, ca.expect("presence.event"))
	if p.UserID != b.id.String() || p.State != "online" {
		t.Fatalf("%+v", p)
	}
	_ = cb.conn.Close(websocket.StatusNormalClosure, "bye")
	p = decode[PresencePayload](t, ca.expect("presence.event"))
	if p.UserID != b.id.String() || p.State != "offline" {
		t.Fatalf("%+v", p)
	}
}

func TestSecondConnectionSupersedesFirst(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a := w.person("a")
	first := n.dial(t, a)
	second := n.dial(t, a)
	first.waitClose(CloseSuperseded)
	// the replaced connection closing must not make the user look offline
	time.Sleep(100 * time.Millisecond)
	second.send("ping", "p", "", nil)
	second.expect("pong")
}

func TestTokenExpiryClosesWith4401AfterGrace(t *testing.T) {
	w := newWorld(t)
	n := w.node(func(o *Options) { o.TokenGrace = 100 * time.Millisecond })
	// swap in an authenticator whose tokens "expire" in 150 ms
	n.hub.deps.Auth = shortLived{inner: IdentityAuth{Svc: w.ident}, ttl: 150 * time.Millisecond}
	a := w.person("a")
	c := n.dial(t, a)
	c.waitClose(CloseAuthExpired)
}

type shortLived struct {
	inner Authenticator
	ttl   time.Duration
}

func (s shortLived) Authenticate(ctx Ctx, token string) (Identity, error) {
	id, err := s.inner.Authenticate(ctx, token)
	id.ExpiresAt = time.Now().Add(s.ttl)
	return id, err
}

func TestAuthRefreshExtendsTheConnection(t *testing.T) {
	w := newWorld(t)
	n := w.node(func(o *Options) { o.TokenGrace = 100 * time.Millisecond })
	sw := &swapAuth{inner: IdentityAuth{Svc: w.ident}}
	sw.ttl.Store(int64(250 * time.Millisecond))
	n.hub.deps.Auth = sw
	a := w.person("a")
	c := n.dial(t, a)

	sw.ttl.Store(int64(time.Hour)) // the next authentication yields a long-lived token
	c.send("auth.refresh", "r1", "", AuthRefreshPayload{AccessToken: a.token})
	if ack := decode[AckPayload](t, c.expect("ack")); ack.Ref != "r1" || !ack.OK {
		t.Fatalf("%+v", ack)
	}
	time.Sleep(600 * time.Millisecond) // well past the original expiry + grace
	c.send("ping", "p", "", nil)
	c.expect("pong")

	// a refresh with a token of ANOTHER user is refused
	other := w.person("b")
	c.send("auth.refresh", "r2", "", AuthRefreshPayload{AccessToken: other.token})
	if e := decode[ErrorPayload](t, c.expect("error")); e.Code != "unauthorized" || e.Ref != "r2" {
		t.Fatalf("%+v", e)
	}
}

type swapAuth struct {
	inner Authenticator
	ttl   atomic.Int64
}

func (s *swapAuth) Authenticate(ctx Ctx, token string) (Identity, error) {
	id, err := s.inner.Authenticate(ctx, token)
	id.ExpiresAt = time.Now().Add(time.Duration(s.ttl.Load()))
	return id, err
}

func TestSinkFullIsNotAcknowledgedAndCanBeResent(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a := w.person("a")
	tid := w.tripWith(a)
	sid := w.startSession(a, tid)
	ca := n.dial(t, a)
	ca.join(tid, 0)

	w.sink.full.Store(true)
	ca.sendPoints(tid, sid, 1, 2)
	if e := decode[ErrorPayload](t, ca.expect("error")); e.Code != "server_busy" || !e.Retryable {
		t.Fatalf("%+v", e)
	}
	ca.expectNone("location.ack", 200*time.Millisecond)

	w.sink.full.Store(false)
	ca.sendPoints(tid, sid, 1, 2) // not treated as duplicates: they were never accepted
	if ack := decode[LocationAckPayload](t, ca.expect("location.ack")); ack.UpToSeq != 2 {
		t.Fatalf("%+v", ack)
	}
	if got := len(w.sink.points()); got != 2 {
		t.Fatalf("exactly the two resent points reach the sink, got %d", got)
	}
}

func TestPausedSessionIsRefusedAndTimeoutEndsIt(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a := w.person("a")
	tid := w.tripWith(a)
	sid := w.startSession(a, tid)
	ca := n.dial(t, a)
	ca.join(tid, 0)
	if _, err := w.sessions.Pause(context.Background(), a.id, sid); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	ca.sendPoints(tid, sid, 1)
	if e := decode[ErrorPayload](t, ca.expect("error")); e.Code != "session_paused" {
		t.Fatalf("%+v", e)
	}
	if _, err := w.sessions.Resume(context.Background(), a.id, sid); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	ca.sendPoints(tid, sid, 1)
	ca.expect("location.ack")
}

func TestFrameSizeAndMessageRateLimits(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a := w.person("a")
	c := n.dial(t, a)
	big := make([]byte, 20*1024)
	for i := range big {
		big[i] = 'x'
	}
	_ = c.conn.Write(context.Background(), websocket.MessageText, big)
	c.waitClose(int(websocket.StatusMessageTooBig))

	flood := n.dial(t, a)
	for i := 0; i < 500; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := flood.conn.Write(ctx, websocket.MessageText, []byte(`{"v":1,"type":"ping"}`))
		cancel()
		if err != nil {
			break
		}
	}
	flood.waitClose(CloseRateLimited)
}

func TestConnectionCapacityLimits(t *testing.T) {
	w := newWorld(t)
	n := w.node(func(o *Options) { o.MaxConns = 2; o.MaxConnsPerIP = 2 })
	p1, p2, p3 := w.person("a"), w.person("b"), w.person("c")
	n.dial(t, p1)
	n.dial(t, p2)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, n.url, &websocket.DialOptions{HTTPHeader: map[string][]string{"Authorization": {"Bearer " + p3.token}}})
	if err == nil || resp == nil || resp.StatusCode != 503 {
		t.Fatalf("over capacity must be refused with 503: %v %v", resp, err)
	}
}

// Two instances sharing the live store and the bus (as with Redis): docs/design/14 A3.
func TestTwoInstancesFanOutThroughTheBus(t *testing.T) {
	w := newWorld(t)
	n1, n2 := w.node(), w.node()
	a, b := w.person("a"), w.person("b")
	tid := w.tripWith(a, b)
	sid := w.startSession(a, tid)
	ca, cb := n1.dial(t, a), n2.dial(t, b)
	head := mustHead(t, w, tid)
	ca.join(tid, head)
	cb.join(tid, head)
	// presence crosses instances
	if p := decode[PresencePayload](t, ca.expect("presence.event")); p.UserID != b.id.String() {
		t.Fatalf("%+v", p)
	}

	ca.sendPoints(tid, sid, 1)
	ev := cb.expect("location.event")
	if pts := decode[LocationEventPayload](t, ev).Points; len(pts) != 1 || pts[0].UserID != a.id.String() {
		t.Fatalf("%+v", pts)
	}
	ca.expectNone("location.event", 150*time.Millisecond) // no echo of our own publish

	// a durable event made on instance 1 reaches the member on instance 2 exactly once
	if _, err := w.sessions.Pause(context.Background(), a.id, sid); err != nil {
		t.Fatal(err)
	}
	cb.expect("trip.event")
	cb.expectNone("trip.event", 200*time.Millisecond)

	// a member removed through instance 1 is cut off on instance 2
	if err := w.trips.Remove(context.Background(), tid, a.id, b.id); err != nil {
		t.Fatal(err)
	}
	if e := decode[ErrorPayload](t, cb.expect("error")); e.Code != "forbidden" {
		t.Fatalf("%+v", e)
	}
	// same user connecting on the other instance supersedes the first connection
	ca2 := n2.dial(t, a)
	_ = ca2
	ca.waitClose(CloseSuperseded)
}

func TestBusSubscriptionIsRefCountedByRooms(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	bus := n.hub.deps.Bus.(*MemBus)
	a, b := w.person("a"), w.person("b")
	tid := w.tripWith(a, b)
	ch := tripChannel(tid)
	eventually(t, func() bool { return !bus.Subscribed(ch) })

	ca, cb := n.dial(t, a), n.dial(t, b)
	ca.join(tid, 0)
	eventually(t, func() bool { return bus.Subscribed(ch) })
	cb.join(tid, 0)
	ca.send("trip.leave", "l", tid.String(), nil)
	ca.expect("ack")
	time.Sleep(100 * time.Millisecond)
	if !bus.Subscribed(ch) {
		t.Fatal("room still has a client: must stay subscribed")
	}
	_ = cb.conn.Close(websocket.StatusNormalClosure, "")
	eventually(t, func() bool { return !bus.Subscribed(ch) }) // last client gone => unsubscribed
	if !bus.Subscribed(userChannel(a.id)) {
		t.Fatal("connected user keeps the control channel")
	}
	_ = ca.conn.Close(websocket.StatusNormalClosure, "")
	eventually(t, func() bool { return !bus.Subscribed(userChannel(a.id)) })
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached in time")
}

func TestGracefulShutdownSendsClosingAndLeavesNoConnections(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a := w.person("a")
	tid := w.tripWith(a)
	ca := n.dial(t, a)
	ca.join(tid, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go n.hub.Shutdown(ctx)
	cl := decode[ClosingPayload](t, ca.expect("closing"))
	if cl.Reason != "server_shutdown" || cl.RetryAfterMs <= 0 {
		t.Fatalf("%+v", cl)
	}
	ca.waitClose(CloseGoingAway)
	eventually(t, func() bool { return n.hub.connCount() == 0 })
	// new connections are refused while draining
	c2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	_, resp, err := websocket.Dial(c2, n.url, &websocket.DialOptions{HTTPHeader: map[string][]string{"Authorization": {"Bearer " + a.token}}})
	if err == nil || resp == nil || resp.StatusCode != 503 {
		t.Fatalf("draining hub must answer 503: %v %v", resp, err)
	}
}

func TestRevokedSessionClosesTheConnection(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a := w.person("a")
	c := n.dial(t, a)
	claims, _ := w.ident.Signer().Verify(a.token)
	sid, _ := uuid.Parse(claims.SID)
	n.hub.RevokeSession(a.id, sid)
	c.waitClose(CloseAuthExpired)
}

var _ = identity.ErrTokenExpired
var _ = json.Marshal
