package realtime

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"
)

// The same two-instance scenario as TestTwoInstancesFanOutThroughTheBus, but over real Redis Pub/Sub and Lua.
func TestTwoInstancesOverRealRedis(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR not set")
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { c.Close() })
	w := newWorld(t)
	w.live = NewRedisStore(c)
	w.newBus = func() Bus { return NewRedisBus(c) }
	n1, n2 := w.node(), w.node()
	a, b := w.person("a"), w.person("b")
	tid := w.tripWith(a, b)
	sid := w.startSession(a, tid)
	head := mustHead(t, w, tid)
	ca, cb := n1.dial(t, a), n2.dial(t, b)
	ca.join(tid, head)
	cb.join(tid, head+1)               // tracking_started is already in the log
	time.Sleep(200 * time.Millisecond) // let both instances' SUBSCRIBE take effect

	ca.sendPoints(tid, sid, 1, 2, 3)
	pts := decode[LocationEventPayload](t, cb.expect("location.event")).Points
	if len(pts) != 1 || pts[0].UserID != a.id.String() || pts[0].Seq < 1 {
		t.Fatalf("%+v", pts)
	}
	ca.expectNone("location.event", 200*time.Millisecond)

	// presence and durable events also cross Redis
	if _, err := w.sessions.Pause(context.Background(), a.id, sid); err != nil {
		t.Fatal(err)
	}
	if k := decode[TripEventPayload](t, cb.expect("trip.event")).Kind; k != "tracking_paused" {
		t.Fatalf("kind %s", k)
	}
	_ = ca.conn.Close(websocket.StatusNormalClosure, "")
	p := decode[PresencePayload](t, cb.expect("presence.event"))
	for p.State != "offline" { // an "online" for a may arrive first
		p = decode[PresencePayload](t, cb.expect("presence.event"))
	}
	if p.UserID != a.id.String() {
		t.Fatalf("%+v", p)
	}
}

// hubGoroutines lists live goroutines that belong to hub/client/shard/ingest code.
func hubGoroutines() []string {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	var out []string
	for _, g := range strings.Split(string(buf), "\n\n") {
		for _, marker := range []string{"realtime.(*Hub)", "realtime.(*Client)", "realtime.(*shard)", "realtime.(*ingest)"} {
			if strings.Contains(g, marker) && !strings.Contains(g, "_test.go") {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

// docs/design/14 A6: disconnects and shutdown leave no ownerless goroutines.
func TestNoHubGoroutineSurvivesShutdown(t *testing.T) {
	w := newWorld(t)
	n := w.node()
	a, b, c := w.person("a"), w.person("b"), w.person("c")
	tid := w.tripWith(a, b, c)
	sid := w.startSession(a, tid)
	ca, cb, cc := n.dial(t, a), n.dial(t, b), n.dial(t, c)
	for _, x := range []*wsClient{ca, cb, cc} {
		x.join(tid, 0)
	}
	ca.sendPoints(tid, sid, 1, 2, 3)
	cb.expect("location.event")
	_ = cb.conn.CloseNow() // abrupt loss of a client
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n.hub.Shutdown(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(hubGoroutines()) > 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if left := hubGoroutines(); len(left) > 0 {
		t.Fatalf("%d hub goroutines still running after shutdown:\n%s", len(left), strings.Join(left, "\n---\n"))
	}
}
