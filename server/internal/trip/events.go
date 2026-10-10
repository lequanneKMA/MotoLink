package trip

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"motolink/internal/platform/id"
)

// Kafka topics for outbox rows (docs/design/09 §2).
const (
	TopicTripEvents = "motolink.trip-events.v1"
	TopicSOS        = "motolink.sos.v1"
)

func topicFor(kind string) string {
	if strings.HasPrefix(kind, "sos_") {
		return TopicSOS
	}
	return TopicTripEvents
}

// AppendEvent writes a durable event inside the caller's transaction (T5, ADR-008):
// bumps trips.event_seq (serialising writers of one trip, so seq is gap-free),
// inserts trip_events and an outbox row. Callers publish the returned Event to
// realtime subscribers after commit as a best-effort fast path; the outbox relay
// guarantees delivery eventually.
func AppendEvent(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, actor *uuid.UUID, kind string, data any) (Event, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return Event{}, fmt.Errorf("marshal event data: %w", err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	ev := Event{ID: id.New(), TripID: tripID, Kind: kind, ActorID: actor, Data: raw}
	if err := tx.QueryRow(ctx,
		`UPDATE trips SET event_seq = event_seq + 1, updated_at = now() WHERE id = $1 RETURNING event_seq`,
		tripID).Scan(&ev.Seq); err != nil {
		return Event{}, fmt.Errorf("bump event_seq: %w", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO trip_events (id, trip_id, seq, type, actor_id, payload) VALUES ($1,$2,$3,$4,$5,$6) RETURNING created_at`,
		ev.ID, tripID, ev.Seq, kind, actor, raw).Scan(&ev.CreatedAt); err != nil {
		return Event{}, fmt.Errorf("insert trip_events: %w", err)
	}
	msg := map[string]any{
		"event_id": ev.ID, "trip_id": tripID, "event_seq": ev.Seq, "kind": kind,
		"actor_id": actor, "data": json.RawMessage(raw), "created_at": ev.CreatedAt.UnixMilli(),
	}
	body, _ := json.Marshal(msg)
	if _, err := tx.Exec(ctx,
		`INSERT INTO outbox (topic, msg_key, event_id, payload) VALUES ($1,$2,$3,$4)`,
		topicFor(kind), tripID.String(), ev.ID, body); err != nil {
		return Event{}, fmt.Errorf("insert outbox: %w", err)
	}
	return ev, nil
}

// EventPublisher receives realtime side effects after a transaction committed.
// The realtime module implements it; the zero value (Noop) is used in tests and
// when the ws role is disabled.
type EventPublisher interface {
	// PublishEvent fans a committed durable event out to connected members (fast path).
	PublishEvent(ctx context.Context, ev Event)
	// MembershipChanged tells connections that a user's rights on a trip changed
	// (left/removed): the hub must drop them from the room (docs/design/07 §7).
	MembershipChanged(ctx context.Context, tripID, userID uuid.UUID, status string)
	// TripEnded clears live state of a trip (Redis locations, rooms).
	TripEnded(ctx context.Context, tripID uuid.UUID)
	// SharingChanged lets the hub stop (or resume) broadcasting a member's location.
	SharingChanged(ctx context.Context, tripID, userID uuid.UUID, sharing bool)
}

type NoopPublisher struct{}

func (NoopPublisher) PublishEvent(context.Context, Event)                             {}
func (NoopPublisher) MembershipChanged(context.Context, uuid.UUID, uuid.UUID, string) {}
func (NoopPublisher) TripEnded(context.Context, uuid.UUID)                            {}
func (NoopPublisher) SharingChanged(context.Context, uuid.UUID, uuid.UUID, bool)      {}

// ListEvents returns events with seq > after, oldest first, at most limit.
func ListEvents(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, tripID uuid.UUID, after int64, limit int, notOlderThan time.Time) ([]Event, error) {
	rows, err := q.Query(ctx,
		`SELECT id, trip_id, seq, type, actor_id, payload, created_at FROM trip_events
		 WHERE trip_id=$1 AND seq > $2 AND created_at >= $3 ORDER BY seq LIMIT $4`,
		tripID, after, notOlderThan, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) {
		var e Event
		err := r.Scan(&e.ID, &e.TripID, &e.Seq, &e.Kind, &e.ActorID, &e.Data, &e.CreatedAt)
		return e, err
	})
}
