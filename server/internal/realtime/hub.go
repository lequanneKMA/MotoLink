package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"motolink/internal/tracking"
	"motolink/internal/trip"
)

type Ctx = context.Context

// Options tune the hub. Zero values get the defaults of docs/design/07 and 10.
type Options struct {
	InstanceID       string
	Shards           int
	IngestWorkers    int
	MaxConns         int
	MaxConnsPerIP    int
	HelloTimeout     time.Duration
	PingInterval     time.Duration
	PongTimeout      time.Duration
	WriteTimeout     time.Duration
	CoalesceEvery    time.Duration
	MaxFrameBytes    int64
	ReapEvery        time.Duration
	ReconcileEvery   time.Duration
	TrustedProxyHops int
	TokenGrace       time.Duration
	Log              *slog.Logger
}

func (o *Options) defaults() {
	if o.InstanceID == "" {
		o.InstanceID = "inst-" + randHex(6)
	}
	if o.Shards <= 0 {
		o.Shards = min(runtime.GOMAXPROCS(0), 8)
	}
	if o.IngestWorkers <= 0 {
		o.IngestWorkers = 4
	}
	if o.MaxConns <= 0 {
		o.MaxConns = 1500
	}
	if o.MaxConnsPerIP <= 0 {
		o.MaxConnsPerIP = 20
	}
	d := func(p *time.Duration, v time.Duration) {
		if *p <= 0 {
			*p = v
		}
	}
	d(&o.HelloTimeout, 5*time.Second)
	d(&o.PingInterval, 20*time.Second)
	d(&o.PongTimeout, 10*time.Second)
	d(&o.WriteTimeout, 10*time.Second)
	d(&o.CoalesceEvery, 250*time.Millisecond)
	d(&o.ReapEvery, 10*time.Second)
	d(&o.ReconcileEvery, 30*time.Second)
	d(&o.TokenGrace, 30*time.Second)
	if o.MaxFrameBytes <= 0 {
		o.MaxFrameBytes = 16 * 1024
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
}

// Deps are the collaborators of the hub.
type Deps struct {
	Live     LiveStore
	Bus      Bus
	Trips    *trip.Service
	Sessions *tracking.Service
	Auth     Authenticator
	Sink     Sink
	SOS      SOSHandler
	Metrics  *Metrics
}

// Hub owns every WebSocket connection of this instance (ADR-009).
type Hub struct {
	opt  Options
	deps Deps
	log  *slog.Logger

	shards []*shard
	subCh  chan subCmd
	ingest *ingest
	acks   *ackAggregator

	// user index: one live connection per user (docs/design/08 §4.2). Small, mutex-protected index; the
	// per-trip rooms (the hot path) live in the single-owner shards.
	userMu sync.RWMutex
	users  map[uuid.UUID]*Client

	ipMu    sync.Mutex
	ipConns map[string]int
	conns   atomic.Int64

	draining atomic.Bool
	wg       sync.WaitGroup // all hub goroutines + connections
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewHub(opt Options, d Deps) *Hub {
	opt.defaults()
	if d.Metrics == nil {
		d.Metrics = NewMetrics(nil)
	}
	if d.Bus == nil {
		d.Bus = NoBus{}
	}
	if d.Live == nil {
		d.Live = NewMemStore()
	}
	h := &Hub{opt: opt, deps: d, log: opt.Log.With("module", "realtime", "instance", opt.InstanceID),
		subCh: make(chan subCmd, 256), acks: newAckAggregator(), users: map[uuid.UUID]*Client{}, ipConns: map[string]int{}}
	h.ingest = newIngest(h)
	if d.Sink != nil {
		d.Sink.SetDone(h.acks.Done)
	}
	return h
}

// Start launches all hub goroutines; they stop when ctx ends or Shutdown is called.
func (h *Hub) Start(ctx context.Context) {
	h.ctx, h.cancel = context.WithCancel(ctx)
	for i := 0; i < h.opt.Shards; i++ {
		s := newShard(h, i)
		h.shards = append(h.shards, s)
		h.spawn(s.run)
	}
	h.ingest.start()
	h.spawn(h.subscriber)
	h.spawn(h.busLoop)
	h.spawn(h.janitor)
	h.spawn(h.ackLoop)
}

// spawn runs fn as a hub goroutine with panic containment: a panic in infrastructure code is logged and
// ends the process via the supervisor (restart), since shared state can no longer be trusted (docs/design/10 §1).
func (h *Hub) spawn(fn func(context.Context)) {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		fn(h.ctx)
	}()
}

// Shutdown drains: new connections are refused, every client gets `closing` and a 1001 close, then the
// hub goroutines stop (docs/design/10 §7).
func (h *Hub) Shutdown(ctx context.Context) {
	h.draining.Store(true)
	h.userMu.RLock()
	clients := make([]*Client, 0, len(h.users))
	for _, c := range h.users {
		clients = append(clients, c)
	}
	h.userMu.RUnlock()
	for _, c := range clients {
		c.out.pushCtrl(marshalEnv("closing", "", "", 0, ClosingPayload{Reason: "server_shutdown", RetryAfterMs: 3000 + int64(time.Now().UnixNano()%2000)}))
		c.kick(CloseGoingAway, "server_shutdown")
	}
	done := make(chan struct{})
	go func() {
		for h.conns.Load() > 0 {
			time.Sleep(20 * time.Millisecond)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		h.log.Warn("shutdown timed out with connections open", "open", h.conns.Load())
	}
	h.ingest.stop()
	h.cancel()
	h.wg.Wait()
}

func (h *Hub) shardFor(tripID uuid.UUID) *shard {
	// UUIDv7 puts its randomness in the last bytes
	i := (int(tripID[15]) ^ int(tripID[14])<<3 ^ int(tripID[13])<<6) % len(h.shards)
	return h.shards[i]
}

// ---- user index ----

// register makes c the user's connection and returns the connection it replaced, if any (same process).
func (h *Hub) register(c *Client) (old *Client) {
	h.userMu.Lock()
	old = h.users[c.user]
	h.users[c.user] = c
	h.userMu.Unlock()
	return old
}

func (h *Hub) unregister(c *Client) {
	h.userMu.Lock()
	if h.users[c.user] == c {
		delete(h.users, c.user)
	}
	h.userMu.Unlock()
}

func (h *Hub) clientOf(u uuid.UUID) *Client {
	h.userMu.RLock()
	defer h.userMu.RUnlock()
	return h.users[u]
}

func (h *Hub) connCount() int { return int(h.conns.Load()) }

// ---- bus subscription management (ADR-006: ref-count by room existence) ----

type subCmd struct {
	channel string
	sub     bool
}

func (h *Hub) wantChannel(ch string, sub bool) {
	select {
	case h.subCh <- subCmd{ch, sub}:
	case <-h.ctx.Done():
	}
}

// subscriber is the only goroutine that talks to the bus about subscriptions, so (un)subscribe order is
// preserved per channel. A periodic reconcile heals any drift from lost commands or failed calls.
func (h *Hub) subscriber(ctx context.Context) {
	have := map[string]bool{}
	tick := time.NewTicker(h.opt.ReconcileEvery)
	defer tick.Stop()
	apply := func(ch string, sub bool) {
		var err error
		if sub && !have[ch] {
			if err = h.deps.Bus.Subscribe(ctx, ch); err == nil {
				have[ch] = true
			}
		} else if !sub && have[ch] {
			if err = h.deps.Bus.Unsubscribe(ctx, ch); err == nil {
				delete(have, ch)
			}
		}
		if err != nil {
			h.log.Warn("bus (un)subscribe failed; reconcile will retry", "channel", ch, "sub", sub, "err", err)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-h.subCh:
			apply(c.channel, c.sub)
		case <-tick.C:
			want := h.desiredChannels(ctx)
			for ch := range want {
				apply(ch, true)
			}
			for ch := range have {
				if !want[ch] {
					apply(ch, false)
				}
			}
		}
	}
}

func (h *Hub) desiredChannels(ctx context.Context) map[string]bool {
	want := map[string]bool{}
	for _, s := range h.shards {
		for _, t := range s.rooms(ctx) {
			want[tripChannel(t)] = true
		}
	}
	h.userMu.RLock()
	for u := range h.users {
		want[userChannel(u)] = true
	}
	h.userMu.RUnlock()
	return want
}

// ---- publishing to the bus ----

func (h *Hub) publishBus(channel string, m busMsg) {
	m.O = h.opt.InstanceID
	b, _ := json.Marshal(m)
	ctx, cancel := context.WithTimeout(h.ctx, 200*time.Millisecond)
	defer cancel()
	if err := h.deps.Bus.Publish(ctx, channel, b); err != nil {
		h.deps.Metrics.pubsubErrors.Inc()
		h.log.Debug("bus publish failed", "channel", channel, "err", err)
	}
}

func (h *Hub) publishUserCtl(u uuid.UUID, m busMsg) {
	m.U = u.String()
	h.publishBus(userChannel(u), m)
}

// busLoop receives cross-instance messages. It never blocks on a slow shard: live data is dropped when the
// shard queue is full (counted), control frames wait briefly.
func (h *Hub) busLoop(ctx context.Context) {
	handle := func(ch string, data []byte) {
		var m busMsg
		if json.Unmarshal(data, &m) != nil || m.O == h.opt.InstanceID {
			return // echo of our own publish: already delivered locally
		}
		switch m.K {
		case kLocation:
			tid, err := uuid.Parse(m.T)
			if err != nil {
				return
			}
			var pts []LivePoint
			if json.Unmarshal(m.P, &pts) != nil {
				return
			}
			for _, p := range pts {
				h.shardFor(tid).tryLoc(cmdLoc{trip: tid, p: p})
			}
		case kFrame:
			tid, err := uuid.Parse(m.T)
			if err != nil {
				return
			}
			var except uuid.UUID
			if m.U != "" {
				except, _ = uuid.Parse(m.U)
			}
			h.shardFor(tid).send(ctx, cmdFrame{trip: tid, frame: []byte(m.F), except: except})
		case kSuperseded:
			if u, err := uuid.Parse(m.U); err == nil {
				if c := h.clientOf(u); c != nil && c.connID == m.C {
					c.kick(CloseSuperseded, "superseded")
				}
			}
		case kMembership:
			u, e1 := uuid.Parse(m.U)
			tid, e2 := uuid.Parse(m.T)
			if e1 == nil && e2 == nil {
				h.localMembershipChanged(ctx, tid, u, m.S)
			}
		case kSharing:
			u, e1 := uuid.Parse(m.U)
			tid, e2 := uuid.Parse(m.T)
			if e1 == nil && e2 == nil {
				h.localSharingChanged(tid, u, m.S == "on")
			}
		case kSession:
			if u, err := uuid.Parse(m.U); err == nil {
				if c := h.clientOf(u); c != nil {
					c.invalidateSessions()
				}
			}
		case kUserFrame:
			if u, err := uuid.Parse(m.U); err == nil {
				if c := h.clientOf(u); c != nil {
					c.push([]byte(m.F))
				}
			}
		case kRevoke:
			if u, err := uuid.Parse(m.U); err == nil {
				if c := h.clientOf(u); c != nil && c.sid.String() == m.S {
					c.out.pushCtrl(errFrame("", "token_expired", "Phiên đã bị thu hồi", false, 0))
					c.kick(CloseAuthExpired, "revoked")
				}
			}
		}
	}
	onGap := func() {
		// Messages may have been lost while the bus was down. Live data heals itself; durable events are
		// recovered by the client via event_seq catch-up, so tell every room to do that.
		h.log.Warn("bus gap detected; asking clients to resync")
		h.deps.Metrics.pubsubReconnects.Inc()
		frame := marshalEnv("resync.required", "", "", 0, ResyncPayload{Reason: "pubsub_gap"})
		for _, s := range h.shards {
			for _, t := range s.rooms(ctx) {
				s.send(ctx, cmdFrame{trip: t, frame: frame})
			}
		}
	}
	if err := h.deps.Bus.Run(ctx, handle, onGap); err != nil && ctx.Err() == nil {
		h.log.Error("bus loop ended", "err", err)
	}
}

// janitor announces users whose presence expired (instance crash, network loss) — exactly one instance wins
// the ZREM per user, so the "offline" frame is not duplicated (docs/design/08 §4.2).
func (h *Hub) janitor(ctx context.Context) {
	tick := time.NewTicker(h.opt.ReapEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			for _, s := range h.shards {
				for _, t := range s.rooms(ctx) {
					c, cancel := context.WithTimeout(ctx, time.Second)
					users, err := h.deps.Live.ReapPresence(c, t)
					cancel()
					if err != nil {
						continue
					}
					for _, u := range users {
						h.announcePresence(t, u, "offline")
					}
				}
			}
		}
	}
}

func (h *Hub) announcePresence(tripID, user uuid.UUID, state string) {
	frame := marshalEnv("presence.event", "", tripID.String(), 0, PresencePayload{UserID: user.String(), State: state, At: time.Now().UnixMilli()})
	h.shardFor(tripID).send(h.ctx, cmdFrame{trip: tripID, frame: frame, except: user})
	h.publishBus(tripChannel(tripID), busMsg{K: kFrame, T: tripID.String(), U: user.String(), F: frame})
}

// ackLoop flushes cumulative location acks to their owners.
func (h *Hub) ackLoop(ctx context.Context) {
	tick := time.NewTicker(h.opt.CoalesceEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			for _, a := range h.acks.collect(30 * time.Minute) {
				frame := marshalEnv("location.ack", "", a.tripID.String(), 0, a.payload)
				a.c.push(frame)
			}
		}
	}
}

// ---- local effects of control events ----

func (h *Hub) localMembershipChanged(ctx context.Context, tripID, user uuid.UUID, status string) {
	if c := h.clientOf(user); c != nil {
		c.dropTrip(tripID)
		c.push(errFrameTrip(tripID, "forbidden", "Bạn không còn là thành viên của chuyến đi này"))
		h.shardFor(tripID).send(ctx, cmdKick{trip: tripID, c: c})
	}
	_ = h.deps.Live.PresenceRemove(ctx, tripID, user)
	_ = h.deps.Live.DeleteLocation(ctx, tripID, user)
}

func (h *Hub) localSharingChanged(tripID, user uuid.UUID, sharing bool) {
	if c := h.clientOf(user); c != nil {
		c.setSharing(tripID, sharing)
	}
}

func errFrameTrip(tripID uuid.UUID, code, msg string) []byte {
	b, _ := json.Marshal(Envelope{V: ProtoVersion, Type: "error", TripID: tripID.String(), Ts: time.Now().UnixMilli(),
		Payload: mustJSON(ErrorPayload{Code: code, Message: msg})})
	return b
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// SetSOS installs the SOS handler (set before Start; the sos service needs the hub for fan-out, so it is wired late).
func (h *Hub) SetSOS(s SOSHandler) { h.deps.SOS = s }
