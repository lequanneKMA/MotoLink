package realtime

import "sync"

const ctrlQueueCap = 64 // docs/design/10 §4: client.ctrl

// outbox is a client's send buffer (ADR-009, docs/design/10 §5.3). Two lanes:
//   - ctrl: durable/control frames, FIFO, bounded; overflow means a slow consumer and the connection is closed
//     (the client resumes by event_seq, nothing durable is lost).
//   - latest: at most one position per member, newest wins. This is the "drop-oldest" policy of live data
//     by construction: memory is bounded by the number of members, however slow the reader is.
type outbox struct {
	mu     sync.Mutex
	ctrl   [][]byte
	latest map[string]LivePoint
	wake   chan struct{}
	closed bool
}

func newOutbox() *outbox {
	return &outbox{latest: map[string]LivePoint{}, wake: make(chan struct{}, 1)}
}

func (o *outbox) signal() {
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

// putLatest stores p unless a newer position of that member is already queued. Reports whether it replaced one.
func (o *outbox) putLatest(p LivePoint) (replaced bool) {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return false
	}
	key := p.TripID + "/" + p.UserID
	cur, ok := o.latest[key]
	if ok && !p.newer(cur) {
		o.mu.Unlock()
		return false
	}
	o.latest[key] = p
	o.mu.Unlock()
	o.signal()
	return ok
}

// pushCtrl queues a frame; overflow=true means the queue is full and the caller must close the connection.
func (o *outbox) pushCtrl(frame []byte) (overflow bool) {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return false
	}
	if len(o.ctrl) >= ctrlQueueCap {
		o.mu.Unlock()
		return true
	}
	o.ctrl = append(o.ctrl, frame)
	o.mu.Unlock()
	o.signal()
	return false
}

// drain takes everything queued.
func (o *outbox) drain() (ctrl [][]byte, loc []LivePoint) {
	o.mu.Lock()
	defer o.mu.Unlock()
	ctrl, o.ctrl = o.ctrl, nil
	if len(o.latest) > 0 {
		loc = make([]LivePoint, 0, len(o.latest))
		for k, p := range o.latest {
			loc = append(loc, p)
			delete(o.latest, k)
		}
	}
	return ctrl, loc
}

func (o *outbox) close() {
	o.mu.Lock()
	o.closed = true
	o.ctrl, o.latest = nil, map[string]LivePoint{}
	o.mu.Unlock()
}

func (o *outbox) depth() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.ctrl)
}
