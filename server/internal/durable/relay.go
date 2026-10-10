package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"motolink/internal/trip"
)

// Message is one outbox row.
type Message struct {
	ID      int64
	Topic   string
	Key     string
	EventID uuid.UUID
	Payload []byte
}

// Publisher ships outbox rows somewhere durable or in-process. It must be idempotent for retries
// (consumers de-duplicate by event_id / event_seq).
type Publisher interface {
	Publish(ctx context.Context, msgs []Message) error
}

// PublisherFunc adapts a function.
type PublisherFunc func(ctx context.Context, msgs []Message) error

func (f PublisherFunc) Publish(ctx context.Context, m []Message) error { return f(ctx, m) }

// Fanout delivers a committed event to connected clients (implemented by realtime.Hub or realtime.BusFanout).
type Fanout interface {
	PublishEvent(ctx context.Context, ev trip.Event)
}

// EventSideEffects reacts to a delivered event (push notifications, ...). It may be nil.
type EventSideEffects interface {
	OnEvent(ctx context.Context, ev trip.Event) error
}

// Dispatcher turns an outbox message into realtime fan-out + side effects. It is the consumer-side of both
// the inproc and Kafka modes, so behaviour is identical (at-least-once; receivers de-duplicate).
type Dispatcher struct {
	Fanout  Fanout
	Effects EventSideEffects
	Log     *slog.Logger
}

type outboxEvent struct {
	EventID   uuid.UUID       `json:"event_id"`
	TripID    uuid.UUID       `json:"trip_id"`
	EventSeq  int64           `json:"event_seq"`
	Kind      string          `json:"kind"`
	ActorID   *uuid.UUID      `json:"actor_id"`
	Data      json.RawMessage `json:"data"`
	CreatedAt int64           `json:"created_at"`
}

// DecodeEvent parses an outbox payload.
func DecodeEvent(payload []byte) (trip.Event, error) {
	var o outboxEvent
	if err := json.Unmarshal(payload, &o); err != nil {
		return trip.Event{}, err
	}
	if o.TripID == uuid.Nil || o.Kind == "" {
		return trip.Event{}, fmt.Errorf("outbox payload without trip_id/kind")
	}
	return trip.Event{ID: o.EventID, TripID: o.TripID, Seq: o.EventSeq, Kind: o.Kind, ActorID: o.ActorID,
		Data: o.Data, CreatedAt: time.UnixMilli(o.CreatedAt)}, nil
}

// Handle processes one message. A malformed payload is logged and dropped (retrying cannot fix it).
func (d *Dispatcher) Handle(ctx context.Context, payload []byte) error {
	ev, err := DecodeEvent(payload)
	if err != nil {
		if d.Log != nil {
			d.Log.Error("dropping malformed outbox payload", "err", err)
		}
		return nil
	}
	if d.Fanout != nil {
		d.Fanout.PublishEvent(ctx, ev)
	}
	if d.Effects != nil {
		return d.Effects.OnEvent(ctx, ev)
	}
	return nil
}

// Publisher returns a Publisher that dispatches in-process (OUTBOX_SINK=inproc, ADR-021).
func (d *Dispatcher) Publisher() Publisher {
	return PublisherFunc(func(ctx context.Context, msgs []Message) error {
		for _, m := range msgs {
			if err := d.Handle(ctx, m.Payload); err != nil {
				return err
			}
		}
		return nil
	})
}

// RelayOptions tune the relay.
type RelayOptions struct {
	Poll      time.Duration
	BatchSize int
	Log       *slog.Logger
	Reg       prometheus.Registerer
}

// Relay polls unpublished outbox rows with FOR UPDATE SKIP LOCKED, so several workers can run safely
// (docs/design/09 §4). Rows are marked published only after the Publisher succeeded: at-least-once.
type Relay struct {
	db   *pgxpool.Pool
	pub  Publisher
	opt  RelayOptions
	wake chan struct{}

	published prometheus.Counter
	failures  prometheus.Counter
	lag       prometheus.Gauge
}

func NewRelay(db *pgxpool.Pool, pub Publisher, opt RelayOptions) *Relay {
	if opt.Poll <= 0 {
		opt.Poll = 500 * time.Millisecond
	}
	if opt.BatchSize <= 0 {
		opt.BatchSize = 100
	}
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	r := &Relay{db: db, pub: pub, opt: opt, wake: make(chan struct{}, 1)}
	r.published = prometheus.NewCounter(prometheus.CounterOpts{Name: "motolink_outbox_published_total", Help: "Outbox rows published."})
	r.failures = prometheus.NewCounter(prometheus.CounterOpts{Name: "motolink_outbox_publish_failures_total", Help: "Outbox publish failures."})
	r.lag = prometheus.NewGauge(prometheus.GaugeOpts{Name: "motolink_outbox_oldest_unpublished_seconds", Help: "Age of the oldest unpublished outbox row."})
	if opt.Reg != nil {
		opt.Reg.MustRegister(r.published, r.failures, r.lag)
	}
	return r
}

// Wake asks the relay to poll now (cheap; used right after a commit in inproc mode).
func (r *Relay) Wake() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Run blocks until ctx ends.
func (r *Relay) Run(ctx context.Context) {
	t := time.NewTicker(r.opt.Poll)
	defer t.Stop()
	for {
		for {
			n, err := r.Once(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				r.failures.Inc()
				r.opt.Log.Error("outbox relay", "err", err)
				break
			}
			if n < r.opt.BatchSize {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-r.wake:
		}
	}
}

// Once publishes at most one batch and returns how many rows it handled.
func (r *Relay) Once(ctx context.Context) (int, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())
	rows, err := tx.Query(ctx,
		`SELECT id, topic, msg_key, event_id, payload FROM outbox WHERE published_at IS NULL
		 ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED`, r.opt.BatchSize)
	if err != nil {
		return 0, err
	}
	msgs, err := pgx.CollectRows(rows, func(rw pgx.CollectableRow) (Message, error) {
		var m Message
		err := rw.Scan(&m.ID, &m.Topic, &m.Key, &m.EventID, &m.Payload)
		return m, err
	})
	if err != nil {
		return 0, err
	}
	if len(msgs) == 0 {
		r.lag.Set(0)
		return 0, tx.Commit(ctx)
	}
	if err := r.pub.Publish(ctx, msgs); err != nil {
		return 0, fmt.Errorf("publish %d rows: %w", len(msgs), err)
	}
	ids := make([]int64, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	if _, err := tx.Exec(ctx, `UPDATE outbox SET published_at = now() WHERE id = ANY($1)`, ids); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	r.published.Add(float64(len(msgs)))
	var age *float64
	_ = r.db.QueryRow(ctx, `SELECT extract(epoch FROM now()-min(created_at)) FROM outbox WHERE published_at IS NULL`).Scan(&age)
	if age != nil {
		r.lag.Set(*age)
	} else {
		r.lag.Set(0)
	}
	return len(msgs), nil
}
