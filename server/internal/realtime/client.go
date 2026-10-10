package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"motolink/internal/platform/apperr"
	"motolink/internal/trip"
)

// Client is one WebSocket connection. Goroutines (docs/design/10 §3): the HTTP handler goroutine becomes the
// readLoop; writeLoop is the only other one. Both end through c.ctx.
type Client struct {
	h       *Hub
	user    uuid.UUID
	userStr string
	connID  string
	sid     uuid.UUID
	ip      string
	ws      *websocket.Conn

	ctx    context.Context
	cancel context.CancelFunc
	out    *outbox
	done   chan struct{} // closed when writeLoop has exited

	expiresAt   atomic.Int64 // unix ms of the access token's exp
	clockOffset int64
	closeCode   atomic.Int32
	closeReason atomic.Value

	msgs *bucket

	closeOnce     sync.Once
	resume        []ResumeEntry
	writerStarted atomic.Bool

	mu       sync.Mutex
	trips    map[uuid.UUID]*tripState
	sessions map[uuid.UUID]*sessState
}

type tripState struct {
	role       string
	sharing    atomic.Bool
	verifiedAt time.Time
}

func (h *Hub) newClient(ws *websocket.Conn, id Identity, ip string) *Client {
	ctx, cancel := context.WithCancel(h.ctx)
	c := &Client{h: h, user: id.UserID, userStr: id.UserID.String(), connID: newConnID(), sid: id.SessionID, ip: ip, ws: ws,
		ctx: ctx, cancel: cancel, out: newOutbox(), done: make(chan struct{}),
		msgs:  newBucket(40, 80),
		trips: map[uuid.UUID]*tripState{}, sessions: map[uuid.UUID]*sessState{}}
	c.expiresAt.Store(id.ExpiresAt.UnixMilli())
	return c
}

func newConnID() string { return "c-" + randHex(10) }

// push queues a control/durable frame; a full queue makes this client a slow consumer: closed with 1013 so it
// reconnects and resumes from its event_seq cursor (docs/design/10 §5.4).
func (c *Client) push(frame []byte) {
	if c.out.pushCtrl(frame) {
		c.h.deps.Metrics.slowClose.Inc()
		c.kick(CloseTryLater, "slow_consumer")
	}
}

// kick ends the connection with the given WebSocket code (first caller wins). The writer flushes what is queued
// and then performs the close handshake; the reader is NOT cancelled (cancelling a read in coder/websocket
// would tear the socket down without a close frame).
func (c *Client) kick(code int, reason string) {
	if c.closeCode.CompareAndSwap(0, int32(code)) {
		c.closeReason.Store(reason)
	}
	c.cancel()
}

func (c *Client) closeInfo() (websocket.StatusCode, string) {
	code := int(c.closeCode.Load())
	if code == 0 {
		return websocket.StatusNormalClosure, ""
	}
	r, _ := c.closeReason.Load().(string)
	return websocket.StatusCode(code), r
}

// ServeHTTP is the GET /v1/ws handler.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.draining.Load() || h.ctx == nil || h.ctx.Err() != nil {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "server shutting down", http.StatusServiceUnavailable)
		return
	}
	ip := clientIP(r, h.trustedHops())
	if !h.admit(ip) {
		h.deps.Metrics.connects.WithLabelValues("rejected_capacity").Inc()
		w.Header().Set("Retry-After", "5")
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	defer h.release(ip)

	token := bearerToken(r)
	idn, err := h.deps.Auth.Authenticate(r.Context(), token)
	if err != nil {
		h.deps.Metrics.connects.WithLabelValues("unauthorized").Inc()
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, `{"code":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		h.deps.Metrics.connects.WithLabelValues("upgrade_failed").Inc()
		return
	}
	ws.SetReadLimit(h.opt.MaxFrameBytes)
	h.wg.Add(1)
	defer h.wg.Done()
	c := h.newClient(ws, idn, ip)
	h.deps.Metrics.connects.WithLabelValues("ok").Inc()
	c.run()
}

func (h *Hub) trustedHops() int { return h.opt.TrustedProxyHops }

func (h *Hub) admit(ip string) bool {
	if int(h.conns.Load()) >= h.opt.MaxConns {
		return false
	}
	h.ipMu.Lock()
	defer h.ipMu.Unlock()
	if h.ipConns[ip] >= h.opt.MaxConnsPerIP {
		return false
	}
	h.ipConns[ip]++
	h.conns.Add(1)
	h.deps.Metrics.connections.Inc()
	return true
}

func (h *Hub) release(ip string) {
	h.ipMu.Lock()
	if h.ipConns[ip] <= 1 {
		delete(h.ipConns, ip)
	} else {
		h.ipConns[ip]--
	}
	h.ipMu.Unlock()
	h.conns.Add(-1)
	h.deps.Metrics.connections.Dec()
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func clientIP(r *http.Request, hops int) string {
	if hops > 0 {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			i := len(parts) - hops
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

// run is the readLoop. It owns the connection's lifetime.
func (c *Client) run() {
	defer c.cleanup()
	if err := c.handshake(); err != nil {
		return
	}
	c.writerStarted.Store(true)
	go c.writeLoop()
	c.push(marshalEnv("welcome", "", "", 0, WelcomePayload{
		ConnID: c.connID, UserID: c.userStr, ServerTimeMs: time.Now().UnixMilli(), ClockOffset: c.clockOffset,
		VMin: ProtoVersion, VMax: ProtoVersion,
		Heartbeat: Heartbeat{PingIntervalS: int(c.h.opt.PingInterval.Seconds()), PongTimeoutS: int(c.h.opt.PongTimeout.Seconds())},
		Limits:    Limits{MaxFrameBytes: int(c.h.opt.MaxFrameBytes), MaxPointsPerFrame: maxPointsPerFrame},
	}))
	for _, r := range c.resume {
		c.safeJoin(r)
	}
	for {
		_, data, err := c.ws.Read(context.Background())
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusMessageTooBig {
				c.kick(CloseTooBig, "frame_too_large")
			} else {
				c.kick(int(websocket.StatusNormalClosure), "")
			}
			return
		}
		if !c.msgs.allow(1) {
			c.push(errFrame("", "rate_limited", "Gửi quá nhanh", true, 1000))
			if c.msgs.strikes.Add(1) > 20 {
				c.kick(CloseRateLimited, "rate_limited")
				return
			}
			continue
		}
		c.dispatch(data)
	}
}

// handshake reads and validates the first message (docs/design/07 §3.1).
func (c *Client) handshake() error {
	// No writer exists yet, so a timer closes the socket properly if hello never comes.
	timer := time.AfterFunc(c.h.opt.HelloTimeout, func() {
		c.kick(CloseBadMessage, "hello_timeout")
		_ = c.ws.Close(websocket.StatusCode(CloseBadMessage), "hello_timeout")
	})
	_, data, err := c.ws.Read(context.Background())
	timer.Stop()
	if err != nil {
		c.kick(CloseBadMessage, "hello_timeout")
		return err
	}
	var env Envelope
	if json.Unmarshal(data, &env) != nil || env.Type != "hello" {
		c.kick(CloseBadMessage, "hello_expected")
		return errors.New("no hello")
	}
	var hp HelloPayload
	if json.Unmarshal(env.Payload, &hp) != nil {
		c.kick(CloseBadMessage, "bad_hello")
		return errors.New("bad hello")
	}
	ok := false
	for _, v := range hp.SupportedV {
		if v == ProtoVersion {
			ok = true
		}
	}
	if !ok {
		_ = c.writeNow(errFrame(env.ID, "upgrade_required", "Cần cập nhật ứng dụng", false, 0))
		c.kick(CloseBadMessage, "upgrade_required")
		return errors.New("version")
	}
	if hp.ClientTimeMs > 0 {
		c.clockOffset = time.Now().UnixMilli() - hp.ClientTimeMs
	}
	c.resume = hp.Resume
	if len(c.resume) > 20 {
		c.resume = c.resume[:20]
	}

	// One live connection per user (docs/design/08 §4.2): a newer one replaces the older, on any instance.
	prev, err := c.h.deps.Live.ClaimConn(c.ctx, c.user, c.h.opt.InstanceID, c.connID)
	if err != nil {
		c.h.log.Warn("claim conn failed; continuing without cross-instance supersede", "err", err)
	}
	if old := c.h.register(c); old != nil {
		old.kick(CloseSuperseded, "superseded")
	}
	if i := strings.IndexByte(prev, '|'); i > 0 && prev[:i] != c.h.opt.InstanceID {
		c.h.publishUserCtl(c.user, busMsg{K: kSuperseded, C: prev[i+1:]})
	}
	c.h.wantChannel(userChannel(c.user), true)
	return nil
}

func (c *Client) dispatch(data []byte) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil || env.V < 1 {
		c.push(errFrame("", "bad_request", "Khung tin không hợp lệ", false, 0))
		return
	}
	switch env.Type {
	case "ping":
		c.push(marshalEnv("pong", "", "", 0, map[string]any{"ref": env.ID, "server_time_ms": time.Now().UnixMilli()}))
	case "auth.refresh":
		c.onAuthRefresh(env)
	case "trip.join":
		c.onTripJoin(env)
	case "trip.leave":
		c.onTripLeave(env)
	case "location.update":
		c.onLocationUpdate(env)
	case "event.ack":
		// cursor lives on the client; nothing to persist (docs/design/07 §3.5)
	case "sos.create":
		c.onSOSCreate(env)
	case "sos.receipt":
		c.onSOSReceipt(env)
	case "hello":
		c.push(errFrame(env.ID, "bad_request", "hello đã được gửi", false, 0))
	default:
		// unknown types are ignored: forward compatibility
	}
}

func (c *Client) onAuthRefresh(env Envelope) {
	var p AuthRefreshPayload
	if json.Unmarshal(env.Payload, &p) != nil || p.AccessToken == "" {
		c.push(errFrame(env.ID, "bad_request", "Thiếu access_token", false, 0))
		return
	}
	idn, err := c.h.deps.Auth.Authenticate(c.ctx, p.AccessToken)
	if err != nil || idn.UserID != c.user {
		c.push(errFrame(env.ID, "unauthorized", "Token không hợp lệ", false, 0))
		if err != nil {
			c.kick(CloseAuthExpired, "invalid_token")
		}
		return
	}
	c.sid = idn.SessionID
	c.expiresAt.Store(idn.ExpiresAt.UnixMilli())
	c.push(ackFrame(env.ID, nil))
}

func (c *Client) onTripJoin(env Envelope) {
	tid, err := uuid.Parse(env.TripID)
	if err != nil {
		c.push(errFrame(env.ID, "bad_request", "trip_id không hợp lệ", false, 0))
		return
	}
	var p TripJoinPayload
	_ = json.Unmarshal(env.Payload, &p)
	if err := c.joinTrip(tid, p.LastEventSeq, env.ID); err != nil {
		c.pushAPIError(env.ID, err)
	}
}

func (c *Client) safeJoin(r ResumeEntry) {
	tid, err := uuid.Parse(r.TripID)
	if err != nil {
		return
	}
	if err := c.joinTrip(tid, r.LastEventSeq, ""); err != nil {
		c.pushAPIError("", err)
	}
}

func (c *Client) onTripLeave(env Envelope) {
	tid, err := uuid.Parse(env.TripID)
	if err != nil {
		return
	}
	if c.dropTrip(tid) {
		c.h.shardFor(tid).send(c.ctx, cmdLeave{c: c, trip: tid})
		_ = c.h.deps.Live.PresenceRemove(c.ctx, tid, c.user)
		c.h.announcePresence(tid, c.user, "offline")
	}
	c.push(ackFrame(env.ID, nil))
}

// pushAPIError maps service errors to WebSocket error codes.
func (c *Client) pushAPIError(ref string, err error) {
	if ae, ok := apperr.As(err); ok {
		code := ae.Code
		switch {
		case ae.Status == http.StatusNotFound || ae.Status == http.StatusForbidden:
			code = "forbidden"
		case ae.Code == "invalid_state":
			code = "invalid_state"
		}
		retry := ae.Status >= 500
		c.push(errFrame(ref, code, ae.Title, retry, 0))
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	c.h.log.Warn("ws handler error", "err", err)
	c.push(errFrame(ref, "internal", "Lỗi hệ thống", true, 2000))
}

// ---- per-trip authorisation (every trip message, docs/design/07 §7) ----

const membershipRecheck = 60 * time.Second

func (c *Client) tripState(ctx context.Context, tripID uuid.UUID) (*tripState, error) {
	c.mu.Lock()
	ts := c.trips[tripID]
	c.mu.Unlock()
	if ts == nil {
		return nil, apperr.Forbidden()
	}
	if time.Since(ts.verifiedAt) > membershipRecheck {
		_, m, err := c.h.deps.Trips.Access(ctx, tripID, c.user)
		if err != nil {
			c.dropTrip(tripID)
			c.h.shardFor(tripID).send(ctx, cmdKick{c: c, trip: tripID})
			return nil, apperr.Forbidden()
		}
		ts.verifiedAt = time.Now()
		ts.sharing.Store(m.SharingState == trip.SharingOn)
	}
	return ts, nil
}

func (c *Client) dropTrip(tripID uuid.UUID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, had := c.trips[tripID]
	delete(c.trips, tripID)
	return had
}

func (c *Client) setSharing(tripID uuid.UUID, on bool) {
	c.mu.Lock()
	ts := c.trips[tripID]
	c.mu.Unlock()
	if ts != nil {
		ts.sharing.Store(on)
	}
}

func (c *Client) joinedTrips() []uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]uuid.UUID, 0, len(c.trips))
	for t := range c.trips {
		out = append(out, t)
	}
	return out
}

func (c *Client) invalidateSessions() {
	c.mu.Lock()
	for _, s := range c.sessions {
		s.fetched = time.Time{}
	}
	c.mu.Unlock()
}

// ---- write side ----

func (c *Client) writeNow(frame []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.h.opt.WriteTimeout)
	defer cancel()
	return c.ws.Write(ctx, websocket.MessageText, frame)
}

func (c *Client) writeLoop() {
	defer close(c.done)
	defer c.closeSocket() // after the final flush: queued frames (e.g. `closing`) go out before the close frame
	flush := time.NewTicker(c.h.opt.CoalesceEvery)
	ping := time.NewTicker(c.h.opt.PingInterval)
	defer flush.Stop()
	defer ping.Stop()
	for {
		select {
		case <-c.ctx.Done():
			c.finalFlush()
			return
		case <-c.out.wake:
			if !c.flushOut() {
				return
			}
		case <-flush.C:
			if time.Now().UnixMilli() > c.expiresAt.Load()+c.h.opt.TokenGrace.Milliseconds() {
				_ = c.writeNow(errFrame("", "token_expired", "Token đã hết hạn", false, 0))
				c.kick(CloseAuthExpired, "token_expired")
				c.finalFlush()
				return
			}
			if !c.flushOut() {
				return
			}
		case <-ping.C:
			if !c.heartbeat() {
				return
			}
		}
	}
}

// flushOut writes queued control frames then one batched location.event. false = write failed (connection dead).
func (c *Client) flushOut() bool {
	ctrl, loc := c.out.drain()
	for _, f := range ctrl {
		if err := c.writeNow(f); err != nil {
			c.kick(CloseTryLater, "write_failed")
			return false
		}
	}
	byTrip := map[string][]LivePoint{}
	for _, p := range loc {
		byTrip[p.TripID] = append(byTrip[p.TripID], p)
	}
	for tripID, pts := range byTrip {
		if err := c.writeNow(marshalEnv("location.event", "", tripID, 0, LocationEventPayload{Points: pts})); err != nil {
			c.kick(CloseTryLater, "write_failed")
			return false
		}
		c.h.deps.Metrics.fanoutLatency.Observe(float64(time.Now().UnixMilli()-pts[0].ServerTs) / 1000)
	}
	return true
}

// closeSocket performs the close handshake with the recorded code. Safe to call from the writer and from cleanup.
func (c *Client) closeSocket() {
	c.closeOnce.Do(func() {
		code, reason := c.closeInfo()
		c.h.deps.Metrics.closes.WithLabelValues(codeLabel(code)).Inc()
		_ = c.ws.Close(code, reason)
	})
}

// finalFlush best-effort delivers what is queued (e.g. `closing`) before the socket closes.
func (c *Client) finalFlush() {
	ctrl, _ := c.out.drain()
	deadline := time.Now().Add(time.Second)
	for _, f := range ctrl {
		if time.Now().After(deadline) {
			return
		}
		if c.writeNow(f) != nil {
			return
		}
	}
}

// heartbeat: protocol ping (detects dead peers), then refresh our claim and presence in every joined trip.
func (c *Client) heartbeat() bool {
	pctx, cancel := context.WithTimeout(c.ctx, c.h.opt.PongTimeout)
	err := c.ws.Ping(pctx)
	cancel()
	if err != nil {
		if c.ctx.Err() == nil {
			c.h.deps.Metrics.closes.WithLabelValues("ping_timeout").Inc()
			c.kick(CloseGoingAway, "ping_timeout")
		}
		return false
	}
	lctx, lcancel := context.WithTimeout(c.ctx, time.Second)
	defer lcancel()
	if ok, err := c.h.deps.Live.TouchConn(lctx, c.user, c.h.opt.InstanceID, c.connID); err == nil && !ok {
		c.kick(CloseSuperseded, "superseded")
		return false
	}
	for _, t := range c.joinedTrips() {
		if on, err := c.h.deps.Live.PresenceTouch(lctx, t, c.user); err == nil && on {
			c.h.announcePresence(t, c.user, "online")
		}
	}
	return true
}

// ---- teardown (order matters, docs/design/10 §6) ----

func (c *Client) cleanup() {
	c.cancel()
	if c.writerStarted.Load() {
		select {
		case <-c.done:
		case <-time.After(2 * time.Second):
		}
	}
	trips := c.joinedTrips()
	// 1. leave the rooms; the shard releases bus subscriptions at ref-count 0
	byShard := map[*shard][]uuid.UUID{}
	for _, t := range trips {
		s := c.h.shardFor(t)
		byShard[s] = append(byShard[s], t)
	}
	for s, ts := range byShard {
		s.sendUnreg(cmdUnreg{c: c, trips: ts})
	}
	c.h.unregister(c)
	c.h.acks.Detach(c)
	c.out.close()
	// 2. presence: only if this connection still owns the user's claim (a replacement keeps it)
	lctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	if removed, err := c.h.deps.Live.ConnLeave(lctx, c.user, c.h.opt.InstanceID, c.connID, trips); err == nil {
		for _, t := range removed {
			c.h.announcePresence(t, c.user, "offline")
		}
	}
	cancel()
	if c.h.clientOf(c.user) == nil {
		c.h.wantChannel(userChannel(c.user), false)
	}
	// 3. the writer already closed the socket; if it never started (failed handshake) do it here
	c.closeSocket()
}
