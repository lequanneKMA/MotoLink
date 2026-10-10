package realtime

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// A shard is the single owner of its rooms (trip -> clients): no locks on the hot path (ADR-009).
// All access goes through the command channels; nothing here ever waits on a client's network I/O.

type cmdJoin struct {
	c    *Client
	trip uuid.UUID
}
type cmdLeave struct {
	c    *Client
	trip uuid.UUID
}
type cmdUnreg struct {
	c     *Client
	trips []uuid.UUID
}
type cmdKick struct {
	c    *Client
	trip uuid.UUID
}
type cmdLoc struct {
	trip uuid.UUID
	p    LivePoint
}
type cmdFrame struct {
	trip   uuid.UUID
	frame  []byte
	except uuid.UUID // user who must not receive it (uuid.Nil = everybody)
}
type cmdRooms struct{ reply chan []uuid.UUID }
type cmdCount struct {
	trip  uuid.UUID
	reply chan int
}

type shard struct {
	h     *Hub
	idx   int
	cmds  chan any      // joins, leaves, frames, rooms queries — may wait briefly
	locs  chan cmdLoc   // live positions — dropped when full
	unreg chan cmdUnreg // never dropped, own queue so it cannot be starved
	room  map[uuid.UUID]map[*Client]struct{}
}

func newShard(h *Hub, idx int) *shard {
	return &shard{h: h, idx: idx, cmds: make(chan any, 1024), locs: make(chan cmdLoc, 4096), unreg: make(chan cmdUnreg, 4096),
		room: map[uuid.UUID]map[*Client]struct{}{}}
}

func (s *shard) send(ctx context.Context, c any) bool {
	t := time.NewTimer(time.Second)
	defer t.Stop()
	select {
	case s.cmds <- c:
		return true
	case <-ctx.Done():
		return false
	case <-t.C:
		s.h.deps.Metrics.dropped.WithLabelValues("shard_cmd_timeout").Inc()
		return false
	}
}

// tryLoc enqueues a live position without waiting: positions are ephemeral, the next one replaces it.
func (s *shard) tryLoc(c cmdLoc) {
	select {
	case s.locs <- c:
	default:
		s.h.deps.Metrics.dropped.WithLabelValues("shard_full").Inc()
	}
}

// sendUnreg must not be lost: it releases references to a dead client. The queue is large; blocking is bounded by hub ctx.
func (s *shard) sendUnreg(c cmdUnreg) {
	select {
	case s.unreg <- c:
	case <-s.h.ctx.Done():
	}
}

func (s *shard) rooms(ctx context.Context) []uuid.UUID {
	reply := make(chan []uuid.UUID, 1)
	if !s.send(ctx, cmdRooms{reply}) {
		return nil
	}
	select {
	case r := <-reply:
		return r
	case <-ctx.Done():
		return nil
	}
}

func (s *shard) clientsIn(ctx context.Context, trip uuid.UUID) int {
	reply := make(chan int, 1)
	if !s.send(ctx, cmdCount{trip, reply}) {
		return -1
	}
	select {
	case n := <-reply:
		return n
	case <-ctx.Done():
		return -1
	}
}

func (s *shard) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case u := <-s.unreg:
			for _, t := range u.trips {
				s.leave(t, u.c)
			}
		case l := <-s.locs:
			for c := range s.room[l.trip] {
				if c.userStr == l.p.UserID {
					continue
				}
				if c.out.putLatest(l.p) {
					s.h.deps.Metrics.coalesced.Inc()
				}
			}
		case m := <-s.cmds:
			switch v := m.(type) {
			case cmdJoin:
				r := s.room[v.trip]
				if r == nil {
					r = map[*Client]struct{}{}
					s.room[v.trip] = r
					s.h.wantChannel(tripChannel(v.trip), true) // 0 -> 1
				}
				r[v.c] = struct{}{}
			case cmdLeave:
				s.leave(v.trip, v.c)
			case cmdKick:
				s.leave(v.trip, v.c)
			case cmdFrame:
				for c := range s.room[v.trip] {
					if v.except != uuid.Nil && c.user == v.except {
						continue
					}
					if c.out.pushCtrl(v.frame) {
						s.h.deps.Metrics.slowClose.Inc()
						c.kick(CloseTryLater, "slow_consumer")
					}
				}
			case cmdRooms:
				out := make([]uuid.UUID, 0, len(s.room))
				for t := range s.room {
					out = append(out, t)
				}
				v.reply <- out
			case cmdCount:
				v.reply <- len(s.room[v.trip])
			}
		}
	}
}

func (s *shard) leave(trip uuid.UUID, c *Client) {
	r := s.room[trip]
	if r == nil {
		return
	}
	delete(r, c)
	if len(r) == 0 {
		delete(s.room, trip)
		s.h.wantChannel(tripChannel(trip), false) // 1 -> 0
	}
}
