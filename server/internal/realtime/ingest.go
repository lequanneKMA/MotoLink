package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"motolink/internal/platform/apperr"
	"motolink/internal/tracking"
)

const (
	maxPointsPerFrame = 20
	maxAccuracyM      = 200.0 // docs/design/07 §8
	maxSpeedMps       = 70.0
	maxJumpMps        = 90.0
	maxClockPast      = 24 * time.Hour
	maxClockFuture    = 5 * time.Second
	sessionRecheck    = 15 * time.Second
	touchEvery        = 30 * time.Second
	ingestQueueCap    = 512
	pointsPerSecond   = 2.0
	pointsBurst       = 40.0
)

// sessState caches what ingest needs about one of the caller's tracking sessions.
type sessState struct {
	id      uuid.UUID
	trip    uuid.UUID
	month   time.Time
	status  string
	fetched time.Time

	lastSeq atomic.Int64
	lastFix struct {
		lat, lng float64
		tsMs     int64
		ok       bool
	}
	bucket    *bucket
	lastTouch time.Time
}

// rewind makes a point that could not be queued look unseen again so that the client's resend is accepted.
func (s *sessState) rewind(seq int64) {
	for {
		cur := s.lastSeq.Load()
		if cur < seq || s.lastSeq.CompareAndSwap(cur, seq-1) {
			return
		}
	}
}

func (c *Client) session(ctx context.Context, sid, tripID uuid.UUID) (*sessState, error) {
	c.mu.Lock()
	s := c.sessions[sid]
	c.mu.Unlock()
	if s != nil && time.Since(s.fetched) < sessionRecheck {
		return s, s.check(c.user, tripID)
	}
	ses, err := c.h.deps.Sessions.LookupOpen(ctx, sid)
	if err != nil {
		return nil, apperr.Conflict("session_unknown", "Phiên ghi không tồn tại")
	}
	if ses.UserID != c.user || ses.TripID != tripID {
		return nil, apperr.Conflict("session_unknown", "Phiên ghi không tồn tại")
	}
	month, _ := time.Parse("2006-01-02", ses.SessionMonth)
	c.mu.Lock()
	if s == nil {
		s = &sessState{id: sid, trip: tripID, bucket: newBucket(pointsPerSecond, pointsBurst), lastTouch: time.Now()}
		s.lastSeq.Store(int64(ses.LastSeq)) // resume point after a server restart
		c.sessions[sid] = s
	}
	s.month, s.status, s.fetched = month, ses.Status, time.Now()
	c.mu.Unlock()
	return s, s.check(c.user, tripID)
}

func (s *sessState) check(user, tripID uuid.UUID) error {
	switch s.status {
	case tracking.StatusActive:
		return nil
	case tracking.StatusPaused:
		return apperr.Conflict("session_paused", "Phiên ghi đang tạm dừng")
	default:
		return apperr.Conflict("session_unknown", "Phiên ghi đã kết thúc")
	}
}

type prepared struct {
	dp     DurablePoint
	live   LivePoint
	liveOK bool   // may be shown to others
	reject string // non-empty: permanently rejected
	seq    int64
}

// onLocationUpdate validates a frame cheaply on the read goroutine and hands it to an ingest worker.
// Nothing here blocks on I/O except the (cached) session lookup (docs/design/10 §9).
func (c *Client) onLocationUpdate(env Envelope) {
	c.h.deps.Metrics.msgsIn.WithLabelValues("location.update").Inc()
	tripID, err := uuid.Parse(env.TripID)
	if err != nil {
		c.push(errFrame(env.ID, "bad_request", "trip_id không hợp lệ", false, 0))
		return
	}
	ts, err := c.tripState(c.ctx, tripID)
	if err != nil {
		c.pushAPIError(env.ID, err)
		return
	}
	var p LocationUpdatePayload
	if json.Unmarshal(env.Payload, &p) != nil || len(p.Points) == 0 || len(p.Points) > maxPointsPerFrame {
		c.push(errFrame(env.ID, "bad_request", "points phải có 1–20 điểm", false, 0))
		return
	}
	sid, err := uuid.Parse(p.SessionID)
	if err != nil {
		c.push(errFrame(env.ID, "bad_request", "session_id không hợp lệ", false, 0))
		return
	}
	sess, err := c.session(c.ctx, sid, tripID)
	if err != nil {
		c.pushAPIError(env.ID, err)
		return
	}

	now := time.Now()
	out := make([]prepared, 0, len(p.Points))
	last := sess.lastSeq.Load()
	fix := sess.lastFix
	var prevSeq int64
	for _, pt := range p.Points {
		if pt.Seq <= prevSeq || pt.Seq <= 0 { // seq must strictly increase inside a frame
			c.push(errFrame(env.ID, "bad_request", "seq phải tăng dần", false, 0))
			return
		}
		prevSeq = pt.Seq
		if pt.Seq <= last { // already seen: resent after a lost ack
			c.h.acks.Duplicate(sid, pt.Seq)
			continue
		}
		if !sess.bucket.allow(1) {
			c.push(errFrame(env.ID, "rate_limited", "Gửi điểm quá nhanh", true, 1000))
			break // the rest is resent later; nothing about them is recorded
		}
		pp := prepared{seq: pt.Seq}
		switch {
		case !validCoord(pt.Lat, pt.Lng):
			pp.reject = "invalid_coordinates"
		case pt.AccuracyM == nil || *pt.AccuracyM <= 0 || float64(*pt.AccuracyM) > maxAccuracyM:
			pp.reject = "low_accuracy"
		}
		if pp.reject == "" {
			suspect := false
			if pt.SpeedMps != nil && (*pt.SpeedMps < 0 || float64(*pt.SpeedMps) > maxSpeedMps) {
				suspect = true
			}
			if fix.ok {
				dt := float64(pt.ClientTs-fix.tsMs) / 1000
				if dt <= 0 {
					dt = 1
				}
				if tracking.Haversine(fix.lat, fix.lng, pt.Lat, pt.Lng)/dt > maxJumpMps {
					suspect = true
				}
			}
			rec := time.UnixMilli(pt.ClientTs + c.clockOffset)
			if pt.ClientTs == 0 || rec.Before(now.Add(-maxClockPast)) || rec.After(now.Add(maxClockFuture)) {
				suspect, rec = true, now
			}
			pp.dp = DurablePoint{
				TripID: tripID, UserID: c.user, SessionID: sid, SessionMonth: sess.month, Seq: pt.Seq,
				RecordedAt: rec, ReceivedAt: now, Lat: pt.Lat, Lng: pt.Lng, AccuracyM: pt.AccuracyM, SpeedMps: pt.SpeedMps,
				HeadingDeg: pt.Heading, AltitudeM: pt.AltitudeM, Suspect: suspect,
			}
			pp.liveOK = !suspect
			pp.live = LivePoint{TripID: tripID.String(), UserID: c.userStr, SessionID: sid.String(), Seq: pt.Seq, Lat: pt.Lat, Lng: pt.Lng,
				AccuracyM: pt.AccuracyM, SpeedMps: pt.SpeedMps, Heading: pt.Heading, ServerTs: now.UnixMilli()}
			if !suspect {
				fix = struct {
					lat, lng float64
					tsMs     int64
					ok       bool
				}{pt.Lat, pt.Lng, pt.ClientTs, true}
			}
		}
		last = pt.Seq
		out = append(out, pp)
	}
	if len(out) == 0 {
		return
	}
	job := ingestJob{c: c, trip: tripID, sess: sess, points: out, sharing: &ts.sharing}
	if !c.h.ingest.submit(job) {
		c.h.deps.Metrics.dropped.WithLabelValues("ingest_full").Inc()
		c.push(errFrame(env.ID, "server_busy", "Máy chủ đang bận, sẽ gửi lại sau", true, 1500))
		return
	}
	sess.lastSeq.Store(last)
	sess.lastFix = fix
}

func validCoord(lat, lng float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lng) && !math.IsInf(lat, 0) && !math.IsInf(lng, 0) &&
		lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180 && !(lat == 0 && lng == 0)
}

// ---- workers ----

type ingestJob struct {
	c       *Client
	trip    uuid.UUID
	sess    *sessState
	points  []prepared
	sharing *atomic.Bool
}

// ingest runs K workers; a user's frames always go to the same worker so per-session order is preserved.
type ingest struct {
	h      *Hub
	queues []chan ingestJob
	wg     sync.WaitGroup
	stopCh chan struct{}
	br     breaker
}

func newIngest(h *Hub) *ingest {
	in := &ingest{h: h, stopCh: make(chan struct{})}
	for i := 0; i < h.opt.IngestWorkers; i++ {
		in.queues = append(in.queues, make(chan ingestJob, ingestQueueCap))
	}
	return in
}

func (in *ingest) start() {
	for i, q := range in.queues {
		in.wg.Add(1)
		go func(i int, q chan ingestJob) {
			defer in.wg.Done()
			g := in.h.deps.Metrics.ingestQueue.WithLabelValues(strconv.Itoa(i))
			for {
				select {
				case j := <-q:
					g.Set(float64(len(q)))
					in.process(j)
				case <-in.stopCh:
					for { // drain what is queued so accepted points still reach the durable branch
						select {
						case j := <-q:
							in.process(j)
						default:
							return
						}
					}
				}
			}
		}(i, q)
	}
}

func (in *ingest) stop() {
	select {
	case <-in.stopCh:
	default:
		close(in.stopCh)
	}
	in.wg.Wait()
}

// submit never blocks: a full queue means the point is not accepted (and not acknowledged).
func (in *ingest) submit(j ingestJob) bool {
	hh := fnv.New32a()
	_, _ = hh.Write(j.c.user[:])
	q := in.queues[int(hh.Sum32())%len(in.queues)]
	select {
	case q <- j:
		return true
	default:
		return false
	}
}

func (in *ingest) process(j ingestJob) {
	h := in.h
	m := h.deps.Metrics
	var newest *LivePoint
	for i := range j.points {
		pp := &j.points[i]
		if pp.reject != "" {
			h.acks.Reject(j.sess.id, j.trip, pp.seq, pp.reject, j.c)
			m.points.WithLabelValues("rejected").Inc()
			continue
		}
		// Register before enqueueing: the sink may report "durable" from another goroutine immediately.
		h.acks.Submitted(j.sess.id, j.trip, pp.seq, j.c)
		if h.deps.Sink == nil {
			h.acks.Done(j.sess.id, pp.seq) // no durable branch configured: nothing to wait for
		} else if err := h.deps.Sink.Enqueue(pp.dp); err != nil {
			j.sess.rewind(pp.seq)
			m.dropped.WithLabelValues("sink_full").Inc()
			code, retry := "server_busy", int64(1500)
			if !errors.Is(err, ErrSinkFull) {
				h.log.Error("sink enqueue failed", "err", err)
			}
			j.c.push(errFrame("", code, "Máy chủ đang bận, sẽ gửi lại sau", true, retry))
			break
		}
		if pp.dp.Suspect {
			m.points.WithLabelValues("suspect").Inc()
		} else {
			m.points.WithLabelValues("accepted").Inc()
		}
		if pp.liveOK {
			lp := pp.live
			newest = &lp
		}
	}
	if time.Since(j.sess.lastTouch) > touchEvery && h.deps.Sessions != nil {
		j.sess.lastTouch = time.Now()
		ctx, cancel := context.WithTimeout(h.ctx, time.Second)
		_ = h.deps.Sessions.Touch(ctx, j.sess.id, j.sess.lastSeq.Load())
		cancel()
	}
	// Live branch: only the newest point matters, and only while the member shares.
	if newest == nil || !j.sharing.Load() {
		return
	}
	in.live(j, *newest)
}

func (in *ingest) live(j ingestJob, lp LivePoint) {
	h := in.h
	accepted := true
	if in.br.allow() {
		ctx, cancel := context.WithTimeout(h.ctx, 100*time.Millisecond)
		ok, err := h.deps.Live.SetLocation(ctx, j.trip, j.c.user, lp)
		cancel()
		if err != nil {
			in.br.fail()
			h.log.Debug("live store unavailable; degraded to local fan-out", "err", err)
		} else {
			in.br.ok()
			accepted = ok
		}
	}
	if !accepted {
		return // an even newer position is already stored
	}
	h.shardFor(j.trip).tryLoc(cmdLoc{trip: j.trip, p: lp})
	b, _ := json.Marshal([]LivePoint{lp})
	h.publishBus(tripChannel(j.trip), busMsg{K: kLocation, T: j.trip.String(), P: b})
}

// breaker opens after 3 consecutive failures for 10 s (docs/design/08 §5): while open the Redis ordering
// check is skipped and positions are fanned out within this instance only.
type breaker struct {
	mu        sync.Mutex
	failures  int
	openUntil time.Time
}

func (b *breaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !time.Now().Before(b.openUntil)
}
func (b *breaker) fail() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures++; b.failures >= 3 {
		b.openUntil = time.Now().Add(10 * time.Second)
		b.failures = 0
	}
}
func (b *breaker) ok() {
	b.mu.Lock()
	b.failures = 0
	b.mu.Unlock()
}

// ---- SOS passthrough ----

func (c *Client) onSOSCreate(env Envelope) {
	if c.h.deps.SOS == nil {
		c.push(errFrame(env.ID, "unsupported", "SOS chưa được bật", false, 0))
		return
	}
	tripID, err := uuid.Parse(env.TripID)
	if err != nil || env.ID == "" {
		c.push(errFrame(env.ID, "bad_request", "Thiếu trip_id hoặc id", false, 0))
		return
	}
	if _, err := c.tripState(c.ctx, tripID); err != nil {
		c.pushAPIError(env.ID, err)
		return
	}
	res, err := c.h.deps.SOS.Create(c.ctx, c.user, tripID, env.ID, env.Payload)
	if err != nil {
		c.pushAPIError(env.ID, err)
		return
	}
	c.push(ackFrame(env.ID, res))
}

func (c *Client) onSOSReceipt(env Envelope) {
	if c.h.deps.SOS == nil {
		return
	}
	tripID, err := uuid.Parse(env.TripID)
	if err != nil {
		return
	}
	if _, err := c.tripState(c.ctx, tripID); err != nil {
		c.pushAPIError(env.ID, err)
		return
	}
	if err := c.h.deps.SOS.Receipt(c.ctx, c.user, tripID, env.Payload); err != nil {
		c.pushAPIError(env.ID, err)
		return
	}
	c.push(ackFrame(env.ID, nil))
}
