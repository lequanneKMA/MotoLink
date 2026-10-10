package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"motolink/internal/identity"
	"motolink/internal/platform/id"
	"motolink/internal/testutil/pgtest"
	"motolink/internal/tracking"
	"motolink/internal/trip"
)

// ---- a fake durable sink whose "durable" moment the test controls ----

type fakeSink struct {
	mu       sync.Mutex
	got      []DurablePoint
	done     func(uuid.UUID, int64)
	auto     atomic.Bool // report durable immediately
	full     atomic.Bool
	enqueued atomic.Int64
}

func (f *fakeSink) SetDone(fn func(uuid.UUID, int64)) { f.done = fn }
func (f *fakeSink) Enqueue(p DurablePoint) error {
	if f.full.Load() {
		return ErrSinkFull
	}
	f.mu.Lock()
	f.got = append(f.got, p)
	f.mu.Unlock()
	f.enqueued.Add(1)
	if f.auto.Load() {
		f.done(p.SessionID, p.Seq)
	}
	return nil
}
func (f *fakeSink) points() []DurablePoint {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]DurablePoint(nil), f.got...)
}
func (f *fakeSink) flush() { // everything enqueued so far becomes durable
	for _, p := range f.points() {
		f.done(p.SessionID, p.Seq)
	}
}

// ---- world: database-backed services + hubs ----

type world struct {
	t        *testing.T
	pool     *pgxpool.Pool
	ident    *identity.Service
	trips    *trip.Service
	sessions *tracking.Service
	live     LiveStore
	group    *MemBusGroup
	sink     *fakeSink
	newBus   func() Bus // default: in-process group
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dsn := pgtest.New(t)
	pool := pgtest.Pool(t, dsn)
	signer, err := identity.NewSigner("", "k1", 15*time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	hasher := identity.NewHasher(identity.ArgonParams{TimeCost: 1, MemoryKiB: 8, Threads: 1, SaltLen: 16, KeyLen: 32, MaxConcurr: 4})
	ident := identity.NewService(identity.Deps{DB: pool, Hasher: hasher, Signer: signer, AccessTTL: 15 * time.Minute,
		RefreshTTL: 24 * time.Hour * 60, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	trips := trip.NewService(pool, nil)
	sessions := tracking.NewService(pool, trips, ident, nil)
	w := &world{t: t, pool: pool, ident: ident, trips: trips, sessions: sessions, live: NewMemStore(), group: NewMemBusGroup(), sink: &fakeSink{}}
	w.newBus = func() Bus { return w.group.New() }
	w.sink.auto.Store(true)
	return w
}

type node struct {
	hub *Hub
	srv *httptest.Server
	url string
}

func (w *world) node(opts ...func(*Options)) *node {
	w.t.Helper()
	o := Options{PingInterval: time.Hour, CoalesceEvery: 20 * time.Millisecond, ReapEvery: time.Hour, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, f := range opts {
		f(&o)
	}
	hub := NewHub(o, Deps{Live: w.live, Bus: w.newBus(), Trips: w.trips, Sessions: w.sessions, Auth: IdentityAuth{Svc: w.ident}, Sink: w.sink})
	hub.Start(context.Background())
	w.trips.SetPublisher(hub)
	w.trips.SetLive(hub)
	w.sessions.SetPublisher(hub)
	srv := httptest.NewServer(hub)
	n := &node{hub: hub, srv: srv, url: "ws" + strings.TrimPrefix(srv.URL, "http")}
	w.t.Cleanup(func() {
		srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hub.Shutdown(ctx)
	})
	return n
}

type person struct {
	id    uuid.UUID
	token string
	name  string
}

func (w *world) person(name string) person {
	w.t.Helper()
	pair, err := w.ident.Register(context.Background(), identity.RegisterInput{
		Email: fmt.Sprintf("%s-%s@x.vn", name, id.NewString()[:8]), Password: "s3cret-pass!", DisplayName: name,
		Consents: []identity.ConsentInput{{Kind: "terms", Version: "1"}, {Kind: "privacy_policy", Version: "1"}},
	})
	if err != nil {
		w.t.Fatal(err)
	}
	if err := w.ident.SetConsent(context.Background(), pair.User.ID, "location_processing", "1", true); err != nil {
		w.t.Fatal(err)
	}
	return person{id: pair.User.ID, token: pair.AccessToken, name: name}
}

// tripWith creates a started trip owned by owner with the given extra members.
func (w *world) tripWith(owner person, members ...person) uuid.UUID {
	w.t.Helper()
	ctx := context.Background()
	tr, err := w.trips.Create(ctx, owner.id, trip.CreateInput{Name: "Đà Lạt"})
	if err != nil {
		w.t.Fatal(err)
	}
	inv, err := w.trips.CreateInvitation(ctx, tr.ID, owner.id, trip.InviteInput{MaxUses: 15})
	if err != nil {
		w.t.Fatal(err)
	}
	for _, m := range members {
		if _, err := w.trips.Join(ctx, m.id, inv.Code); err != nil {
			w.t.Fatal(err)
		}
	}
	if _, err := w.trips.Start(ctx, tr.ID, owner.id); err != nil {
		w.t.Fatal(err)
	}
	return tr.ID
}

func (w *world) startSession(p person, tripID uuid.UUID) uuid.UUID {
	w.t.Helper()
	s, err := w.sessions.Start(context.Background(), p.id, tripID, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	return s.ID
}

// ---- websocket test client ----

type wsClient struct {
	t      *testing.T
	conn   *websocket.Conn
	closed chan websocket.StatusCode
	who    string

	mu      sync.Mutex
	backlog []Envelope
	notify  chan struct{}
	dead    bool
}

func (n *node) dial(t *testing.T, p person, resume ...ResumeEntry) *wsClient {
	t.Helper()
	return n.dialWith(t, p.token, p.name, true, resume...)
}

func (n *node) dialWith(t *testing.T, token, who string, sendHello bool, resume ...ResumeEntry) *wsClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, n.url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
	if err != nil {
		t.Fatalf("dial %s: %v", who, err)
	}
	c := &wsClient{t: t, conn: conn, closed: make(chan websocket.StatusCode, 1), who: who, notify: make(chan struct{}, 1)}
	go func() {
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				c.mu.Lock()
				c.dead = true
				c.mu.Unlock()
				c.closed <- websocket.CloseStatus(err)
				c.wake()
				return
			}
			var e Envelope
			if json.Unmarshal(data, &e) == nil {
				c.mu.Lock()
				c.backlog = append(c.backlog, e)
				c.mu.Unlock()
				c.wake()
			}
		}
	}()
	t.Cleanup(func() { _ = conn.CloseNow() })
	if sendHello {
		c.send("hello", "h1", "", HelloPayload{DeviceID: "dev", AppVersion: "t", OS: "test", ClientTimeMs: time.Now().UnixMilli(), SupportedV: []int{1}, Resume: resume})
		c.expect("welcome")
	}
	return c
}

func (c *wsClient) wake() {
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

func (c *wsClient) send(typ, idv, tripID string, payload any) {
	c.t.Helper()
	b, _ := json.Marshal(payload)
	env, _ := json.Marshal(Envelope{V: 1, Type: typ, ID: idv, TripID: tripID, Payload: b})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, env); err != nil {
		c.t.Fatalf("%s send %s: %v", c.who, typ, err)
	}
}

// take removes and returns the first backlog frame of typ.
func (c *wsClient) take(typ string) (Envelope, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, e := range c.backlog {
		if e.Type == typ {
			c.backlog = append(c.backlog[:i:i], c.backlog[i+1:]...)
			return e, true, c.dead
		}
	}
	return Envelope{}, false, c.dead
}

// expect waits for a frame of the given type. Frames of other types stay queued for later expects.
func (c *wsClient) expect(typ string) Envelope {
	c.t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if e, ok, dead := c.take(typ); ok {
			return e
		} else if dead {
			c.t.Fatalf("%s: connection closed while waiting for %s", c.who, typ)
		}
		select {
		case <-c.notify:
		case <-deadline:
			c.t.Fatalf("%s: timed out waiting for %s", c.who, typ)
		}
	}
}

// expectNone asserts that no frame of typ shows up within d (other frames are left alone).
func (c *wsClient) expectNone(typ string, d time.Duration) {
	c.t.Helper()
	time.Sleep(d)
	if e, ok, _ := c.take(typ); ok {
		c.t.Fatalf("%s: unexpected %s: %s", c.who, typ, e.Payload)
	}
}

func (c *wsClient) waitClose(want int) {
	c.t.Helper()
	select {
	case got := <-c.closed:
		if int(got) != want {
			c.t.Fatalf("%s: closed with %d, want %d", c.who, got, want)
		}
	case <-time.After(5 * time.Second):
		c.t.Fatalf("%s: not closed (want %d)", c.who, want)
	}
}

func (c *wsClient) join(tripID uuid.UUID, last int64) TripJoinedPayload {
	c.t.Helper()
	c.send("trip.join", "j1", tripID.String(), TripJoinPayload{LastEventSeq: last})
	e := c.expect("trip.joined")
	var p TripJoinedPayload
	_ = json.Unmarshal(e.Payload, &p)
	return p
}

func accuracy(v float32) *float32 { return &v }

func (c *wsClient) sendPoints(tripID, sessionID uuid.UUID, seqs ...int64) {
	c.t.Helper()
	pts := make([]WirePoint, 0, len(seqs))
	for _, s := range seqs {
		pts = append(pts, WirePoint{Seq: s, ClientTs: time.Now().UnixMilli(), Lat: 11.94 + float64(s)*0.00001, Lng: 108.45, AccuracyM: accuracy(8)})
	}
	c.send("location.update", "", tripID.String(), LocationUpdatePayload{SessionID: sessionID.String(), Points: pts})
}

func decode[T any](t *testing.T, e Envelope) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(e.Payload, &v); err != nil {
		t.Fatalf("decode %s: %v", e.Type, err)
	}
	return v
}
