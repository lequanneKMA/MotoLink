package realtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Bus is the cross-instance Pub/Sub transport (ADR-006, docs/design/08 §3). It is best effort: messages can be
// lost, which live data tolerates and durable data recovers from through event_seq catch-up.
type Bus interface {
	Publish(ctx context.Context, channel string, data []byte) error
	Subscribe(ctx context.Context, channel string) error
	Unsubscribe(ctx context.Context, channel string) error
	// Run delivers messages to handler until ctx ends. onGap is called after a reconnect during which
	// messages may have been missed.
	Run(ctx context.Context, handler func(channel string, data []byte), onGap func()) error
}

func tripChannel(t uuid.UUID) string { return "ml:ps:trip:{" + t.String() + "}" }
func userChannel(u uuid.UUID) string { return "ml:ps:user:" + u.String() }

// busMsg is the envelope of every bus message. Origin lets an instance skip its own echoes.
type busMsg struct {
	O string          `json:"o"`           // origin instance
	K string          `json:"k"`           // kind
	T string          `json:"t,omitempty"` // trip id
	U string          `json:"u,omitempty"` // user id (control)
	F json.RawMessage `json:"f,omitempty"` // a complete, ready-to-send frame
	P json.RawMessage `json:"p,omitempty"` // kind-specific payload
	S string          `json:"s,omitempty"` // status / sub-kind
	C string          `json:"c,omitempty"` // connection id (superseded)
}

const (
	kLocation   = "loc"
	kFrame      = "frame"      // durable event / presence frame for everybody in the trip
	kSuperseded = "superseded" // user ctl: close that connection with 4409
	kMembership = "membership" // user ctl: membership of a trip changed
	kSharing    = "sharing"    // user ctl: sharing flag changed
	kSession    = "session"    // user ctl: tracking session state changed
	kRevoke     = "revoke"     // user ctl: auth session revoked
)

// ---- Redis ----

type RedisBus struct {
	c  redis.UniversalClient
	ps *redis.PubSub
	mu sync.Mutex
}

func NewRedisBus(c redis.UniversalClient) *RedisBus { return &RedisBus{c: c} }

func (b *RedisBus) Publish(ctx context.Context, ch string, data []byte) error {
	return b.c.Publish(ctx, ch, data).Err()
}

func (b *RedisBus) pubsub(ctx context.Context) *redis.PubSub {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ps == nil {
		b.ps = b.c.Subscribe(ctx) // no channels yet; they are added dynamically by ref-count
	}
	return b.ps
}

func (b *RedisBus) Subscribe(ctx context.Context, ch string) error {
	return b.pubsub(ctx).Subscribe(ctx, ch)
}
func (b *RedisBus) Unsubscribe(ctx context.Context, ch string) error {
	return b.pubsub(ctx).Unsubscribe(ctx, ch)
}

func (b *RedisBus) Run(ctx context.Context, handler func(string, []byte), onGap func()) error {
	ps := b.pubsub(ctx)
	defer ps.Close()
	seen := map[string]bool{}
	in := ps.ChannelWithSubscriptions(redis.WithChannelSize(4096))
	for {
		select {
		case <-ctx.Done():
			return nil
		case m, ok := <-in:
			if !ok {
				return nil
			}
			switch v := m.(type) {
			case *redis.Message:
				handler(v.Channel, []byte(v.Payload))
			case *redis.Subscription:
				// A second "subscribe" for a channel we already hold means go-redis reconnected and
				// re-subscribed: messages may have been lost in between.
				if v.Kind == "subscribe" {
					if seen[v.Channel] && onGap != nil {
						onGap()
					}
					seen[v.Channel] = true
				} else if v.Kind == "unsubscribe" {
					delete(seen, v.Channel)
				}
			}
		}
	}
}

// ---- in-process group (tests, and several hubs inside one process) ----

type MemBusGroup struct {
	mu   sync.Mutex
	subs map[string]map[*MemBus]struct{}
}

func NewMemBusGroup() *MemBusGroup { return &MemBusGroup{subs: map[string]map[*MemBus]struct{}{}} }

func (g *MemBusGroup) New() *MemBus {
	return &MemBus{g: g, inbox: make(chan memMsg, 4096), mine: map[string]bool{}}
}

type memMsg struct {
	ch   string
	data []byte
}

type MemBus struct {
	g     *MemBusGroup
	inbox chan memMsg
	mu    sync.Mutex
	mine  map[string]bool
}

func (b *MemBus) Publish(_ context.Context, ch string, data []byte) error {
	b.g.mu.Lock()
	targets := make([]*MemBus, 0, len(b.g.subs[ch]))
	for t := range b.g.subs[ch] {
		targets = append(targets, t)
	}
	b.g.mu.Unlock()
	for _, t := range targets {
		select {
		case t.inbox <- memMsg{ch, data}:
		default: // full inbox: drop, like a lagging subscriber
		}
	}
	return nil
}

func (b *MemBus) Subscribe(_ context.Context, ch string) error {
	b.g.mu.Lock()
	defer b.g.mu.Unlock()
	if b.g.subs[ch] == nil {
		b.g.subs[ch] = map[*MemBus]struct{}{}
	}
	b.g.subs[ch][b] = struct{}{}
	b.mu.Lock()
	b.mine[ch] = true
	b.mu.Unlock()
	return nil
}

func (b *MemBus) Unsubscribe(_ context.Context, ch string) error {
	b.g.mu.Lock()
	defer b.g.mu.Unlock()
	delete(b.g.subs[ch], b)
	b.mu.Lock()
	delete(b.mine, ch)
	b.mu.Unlock()
	return nil
}

func (b *MemBus) Subscribed(ch string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.mine[ch]
}

func (b *MemBus) Run(ctx context.Context, handler func(string, []byte), _ func()) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case m := <-b.inbox:
			handler(m.ch, m.data)
		}
	}
}

// NoBus is used when Redis is not configured: nothing crosses instances (single-instance deployments).
type NoBus struct{}

func (NoBus) Publish(context.Context, string, []byte) error { return nil }
func (NoBus) Subscribe(context.Context, string) error       { return nil }
func (NoBus) Unsubscribe(context.Context, string) error     { return nil }
func (NoBus) Run(ctx context.Context, _ func(string, []byte), _ func()) error {
	<-ctx.Done()
	return nil
}

var _ = time.Second
