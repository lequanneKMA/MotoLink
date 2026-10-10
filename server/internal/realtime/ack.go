package realtime

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// ackAggregator turns per-point "durable" notifications into cumulative location.ack frames:
// up_to_seq only advances over a contiguous run of points that are durable or permanently rejected
// (docs/design/07 §3.3, ADR-005). Memory is bounded by open sessions.
type ackAggregator struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*ackState
	now      func() time.Time
}

type ackState struct {
	inited     bool
	watermark  int64
	done       map[int64]struct{}
	rejected   map[int64]string // terminal, waiting to be reported once
	reported   []RejectedPoint
	owner      *Client
	tripID     uuid.UUID
	dirty      bool
	lastActive time.Time
}

func newAckAggregator() *ackAggregator {
	return &ackAggregator{sessions: map[uuid.UUID]*ackState{}, now: time.Now}
}

func (a *ackAggregator) state(sid uuid.UUID) *ackState {
	s := a.sessions[sid]
	if s == nil {
		s = &ackState{done: map[int64]struct{}{}, rejected: map[int64]string{}}
		a.sessions[sid] = s
	}
	return s
}

// Submitted registers a point that entered the durable branch. The first seq ever seen for a session
// after a (re)start defines the base: clients resend from their lowest unacked point.
func (a *ackAggregator) Submitted(sid, tripID uuid.UUID, seq int64, c *Client) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.state(sid)
	if !s.inited {
		s.inited, s.watermark = true, seq-1
	}
	s.owner, s.tripID, s.lastActive = c, tripID, a.now()
}

// Done marks a point durable.
func (a *ackAggregator) Done(sid uuid.UUID, seq int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[sid]
	if s == nil || !s.inited || seq <= s.watermark {
		return
	}
	s.done[seq] = struct{}{}
	a.advance(s)
}

// Reject marks a point permanently rejected (the client may drop it).
func (a *ackAggregator) Reject(sid, tripID uuid.UUID, seq int64, reason string, c *Client) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.state(sid)
	if !s.inited {
		s.inited, s.watermark = true, seq-1
	}
	s.owner, s.tripID, s.lastActive = c, tripID, a.now()
	if seq <= s.watermark {
		s.dirty = true
		return
	}
	s.rejected[seq] = reason
	a.advance(s)
}

// Duplicate is called when a point at or below the watermark is received again: the client evidently
// missed our ack, so repeat it.
func (a *ackAggregator) Duplicate(sid uuid.UUID, seq int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.sessions[sid]; s != nil && seq <= s.watermark {
		s.dirty = true
	}
}

func (a *ackAggregator) advance(s *ackState) {
	moved := false
	for {
		next := s.watermark + 1
		if _, ok := s.done[next]; ok {
			delete(s.done, next)
		} else if r, ok := s.rejected[next]; ok {
			delete(s.rejected, next)
			s.reported = append(s.reported, RejectedPoint{Seq: next, Reason: r})
		} else {
			break
		}
		s.watermark = next
		moved = true
	}
	if moved {
		s.dirty = true
	}
}

type pendingAck struct {
	c       *Client
	tripID  uuid.UUID
	session uuid.UUID
	payload LocationAckPayload
}

// collect returns the acks to send now and clears their dirty flags; also forgets idle sessions.
func (a *ackAggregator) collect(idleAfter time.Duration) []pendingAck {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []pendingAck
	now := a.now()
	for sid, s := range a.sessions {
		if s.dirty && s.owner != nil {
			out = append(out, pendingAck{c: s.owner, tripID: s.tripID, session: sid,
				payload: LocationAckPayload{SessionID: sid.String(), UpToSeq: s.watermark, Rejected: s.reported}})
			s.reported = nil
			s.dirty = false
		}
		if now.Sub(s.lastActive) > idleAfter && len(s.done) == 0 && len(s.rejected) == 0 {
			delete(a.sessions, sid)
		}
	}
	return out
}

// Forget drops a session (it ended).
func (a *ackAggregator) Forget(sid uuid.UUID) {
	a.mu.Lock()
	delete(a.sessions, sid)
	a.mu.Unlock()
}

// Detach clears the owner when its connection closes so acks are not sent into the void.
func (a *ackAggregator) Detach(c *Client) {
	a.mu.Lock()
	for _, s := range a.sessions {
		if s.owner == c {
			s.owner = nil
		}
	}
	a.mu.Unlock()
}

func (a *ackAggregator) openSessions() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.sessions)
}
