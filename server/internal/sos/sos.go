// Package sos implements the emergency flow of ADR-015: one transaction (T8) stores the SOS, its recipients and the
// durable trip event; delivery (WebSocket, push) and receipts follow.
package sos

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"motolink/internal/platform/apperr"
	"motolink/internal/platform/id"
	"motolink/internal/trip"
)

const (
	StatusOpen      = "open"
	StatusResolved  = "resolved"
	StatusCancelled = "cancelled"
)

var validKinds = map[string]bool{"general": true, "accident": true, "breakdown": true, "lost": true, "medical": true}

// StatusPusher tells the sender how far their SOS got (sos.status). Optional.
type StatusPusher interface {
	PushSOSStatus(ctx context.Context, sender, tripID uuid.UUID, st Aggregate)
}

// Aggregate is the delivery summary shown to the sender.
type Aggregate struct {
	SOSID      uuid.UUID `json:"sos_id"`
	Total      int       `json:"total"`
	Received   int       `json:"received"`
	Seen       int       `json:"seen"`
	PushFailed int       `json:"push_failed"`
}

type Service struct {
	db     *pgxpool.Pool
	trips  *trip.Service
	pub    trip.EventPublisher
	status StatusPusher
}

func NewService(db *pgxpool.Pool, trips *trip.Service, pub trip.EventPublisher) *Service {
	if pub == nil {
		pub = trip.NoopPublisher{}
	}
	return &Service{db: db, trips: trips, pub: pub}
}

func (s *Service) SetPublisher(p trip.EventPublisher) { s.pub = p }
func (s *Service) SetStatusPusher(p StatusPusher)     { s.status = p }

// CreateInput is the client's SOS description.
type CreateInput struct {
	ClientSOSID     uuid.UUID `json:"client_sos_id"`
	Kind            string    `json:"kind"`
	Message         *string   `json:"message"`
	Lat             *float64  `json:"lat"`
	Lng             *float64  `json:"lng"`
	AccuracyM       *float32  `json:"accuracy_m"`
	ClientCreatedAt *int64    `json:"client_created_at"` // epoch ms
}

// Result of Create.
type Result struct {
	SOSID     uuid.UUID `json:"sos_id"`
	EventSeq  int64     `json:"event_seq"`
	Duplicate bool      `json:"duplicate"`
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := apperr.As(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return apperr.Internal(err)
}

// Create stores an SOS idempotently on (user, client_sos_id).
func (s *Service) Create(ctx context.Context, userID, tripID uuid.UUID, in CreateInput) (*Result, error) {
	if in.ClientSOSID == uuid.Nil {
		return nil, apperr.BadRequest("client_sos_id là bắt buộc")
	}
	if in.Kind == "" {
		in.Kind = "general"
	}
	if !validKinds[in.Kind] {
		return nil, apperr.BadRequest("kind không hợp lệ")
	}
	if in.Message != nil && len([]rune(*in.Message)) > 280 {
		return nil, apperr.BadRequest("message tối đa 280 ký tự")
	}
	if (in.Lat == nil) != (in.Lng == nil) || (in.Lat != nil && (*in.Lat < -90 || *in.Lat > 90 || *in.Lng < -180 || *in.Lng > 180)) {
		return nil, apperr.BadRequest("tọa độ không hợp lệ")
	}
	t, m, err := s.trips.Access(ctx, tripID, userID)
	if err != nil {
		return nil, err
	}
	_ = m
	if t.Status != trip.StatusActive {
		// A retried SOS for a trip that ended meanwhile must still answer as duplicate, not fail.
		if r, err := s.existing(ctx, userID, in.ClientSOSID); err == nil {
			return r, nil
		}
		return nil, apperr.Conflict("trip_not_active", "Chuyến đi không ở trạng thái hoạt động")
	}

	var (
		res Result
		ev  trip.Event
	)
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// Lock the trip row so a concurrent end/leave cannot interleave (same order as AppendEvent).
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM trips WHERE id=$1 FOR UPDATE`, tripID).Scan(&status); err != nil {
			return err
		}
		var isMember bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trip_members WHERE trip_id=$1 AND user_id=$2 AND status='active')`,
			tripID, userID).Scan(&isMember); err != nil {
			return err
		}
		if !isMember {
			return apperr.NotFound()
		}
		var createdAt time.Time
		sosID := id.New()
		var cca *time.Time
		if in.ClientCreatedAt != nil {
			v := time.UnixMilli(*in.ClientCreatedAt)
			cca = &v
		}
		tag, err := tx.Exec(ctx, `INSERT INTO sos_events (id, trip_id, user_id, client_sos_id, kind, message, lat, lng, accuracy_m, client_created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (user_id, client_sos_id) DO NOTHING`,
			sosID, tripID, userID, in.ClientSOSID, in.Kind, in.Message, in.Lat, in.Lng, in.AccuracyM, cca)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 { // retry of an SOS we already have
			r, err := s.existingTx(ctx, tx, userID, in.ClientSOSID)
			if err != nil {
				return err
			}
			res = *r
			return nil
		}
		if err := tx.QueryRow(ctx, `SELECT created_at FROM sos_events WHERE id=$1`, sosID).Scan(&createdAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sos_recipients (sos_id, user_id, push_status)
			SELECT $1, user_id, 'pending' FROM trip_members WHERE trip_id=$2 AND status='active' AND user_id<>$3`,
			sosID, tripID, userID); err != nil {
			return err
		}
		var name string
		_ = tx.QueryRow(ctx, `SELECT display_name FROM users WHERE id=$1`, userID).Scan(&name)
		ev, err = trip.AppendEvent(ctx, tx, tripID, &userID, trip.KindSOSCreated, map[string]any{
			"sos_id": sosID, "sender_id": userID, "sender_name": name, "kind": in.Kind, "message": in.Message,
			"lat": in.Lat, "lng": in.Lng, "accuracy_m": in.AccuracyM, "created_at": createdAt.UnixMilli(),
		})
		if err != nil {
			return err
		}
		res = Result{SOSID: sosID, EventSeq: ev.Seq}
		return nil
	})
	if err != nil {
		return nil, wrap(err)
	}
	if !res.Duplicate && ev.ID != uuid.Nil {
		s.pub.PublishEvent(ctx, ev)
	}
	return &res, nil
}

func (s *Service) existing(ctx context.Context, userID, clientID uuid.UUID) (*Result, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	return s.existingTx(ctx, tx, userID, clientID)
}

func (s *Service) existingTx(ctx context.Context, tx pgx.Tx, userID, clientID uuid.UUID) (*Result, error) {
	var r Result
	err := tx.QueryRow(ctx, `SELECT e.id, COALESCE((SELECT seq FROM trip_events te WHERE te.trip_id=e.trip_id AND te.type='sos_created'
		AND te.payload->>'sos_id'=e.id::text),0) FROM sos_events e WHERE e.user_id=$1 AND e.client_sos_id=$2`, userID, clientID).Scan(&r.SOSID, &r.EventSeq)
	if err != nil {
		return nil, err
	}
	r.Duplicate = true
	return &r, nil
}

// Receipt marks an SOS received/seen by a recipient (idempotent, monotonic: seen implies received).
func (s *Service) Receipt(ctx context.Context, userID, sosID uuid.UUID, status string) error {
	if status != "received" && status != "seen" {
		return apperr.BadRequest("status phải là received hoặc seen")
	}
	var sender, tripID uuid.UUID
	err := s.db.QueryRow(ctx, `UPDATE sos_recipients r SET
			received_at = COALESCE(r.received_at, now()),
			seen_at = CASE WHEN $3='seen' THEN COALESCE(r.seen_at, now()) ELSE r.seen_at END
		FROM sos_events e WHERE r.sos_id=$1 AND r.user_id=$2 AND e.id=r.sos_id RETURNING e.user_id, e.trip_id`,
		sosID, userID, status).Scan(&sender, &tripID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound()
	}
	if err != nil {
		return wrap(err)
	}
	if s.status != nil {
		if ag, err := s.aggregate(ctx, sosID); err == nil {
			s.status.PushSOSStatus(ctx, sender, tripID, ag)
		}
	}
	return nil
}

func (s *Service) aggregate(ctx context.Context, sosID uuid.UUID) (Aggregate, error) {
	ag := Aggregate{SOSID: sosID}
	err := s.db.QueryRow(ctx, `SELECT count(*), count(received_at), count(seen_at), count(*) FILTER (WHERE push_status='failed')
		FROM sos_recipients WHERE sos_id=$1`, sosID).Scan(&ag.Total, &ag.Received, &ag.Seen, &ag.PushFailed)
	return ag, err
}

// SOS is the API view.
type SOS struct {
	ID         uuid.UUID   `json:"id"`
	TripID     uuid.UUID   `json:"trip_id"`
	SenderID   uuid.UUID   `json:"sender_id"`
	SenderName string      `json:"sender_name"`
	Kind       string      `json:"kind"`
	Message    *string     `json:"message,omitempty"`
	Lat        *float64    `json:"lat,omitempty"`
	Lng        *float64    `json:"lng,omitempty"`
	AccuracyM  *float32    `json:"accuracy_m,omitempty"`
	Status     string      `json:"status"`
	ResolvedBy *uuid.UUID  `json:"resolved_by,omitempty"`
	ResolvedAt *time.Time  `json:"resolved_at,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	Delivery   *Aggregate  `json:"delivery,omitempty"`
	Recipients []Recipient `json:"recipients,omitempty"` // only for the sender
}

type Recipient struct {
	UserID     uuid.UUID  `json:"user_id"`
	ReceivedAt *time.Time `json:"received_at"`
	SeenAt     *time.Time `json:"seen_at"`
	PushStatus *string    `json:"push_status"`
}

const sosCols = `e.id, e.trip_id, e.user_id, u.display_name, e.kind, e.message, e.lat, e.lng, e.accuracy_m, e.status, e.resolved_by, e.resolved_at, e.created_at`

func scanSOS(r pgx.Row) (*SOS, error) {
	var x SOS
	err := r.Scan(&x.ID, &x.TripID, &x.SenderID, &x.SenderName, &x.Kind, &x.Message, &x.Lat, &x.Lng, &x.AccuracyM, &x.Status, &x.ResolvedBy, &x.ResolvedAt, &x.CreatedAt)
	return &x, err
}

// Get returns one SOS to a trip member (404 for non-members). The sender also gets per-recipient detail.
func (s *Service) Get(ctx context.Context, userID, sosID uuid.UUID) (*SOS, error) {
	x, err := scanSOS(s.db.QueryRow(ctx, `SELECT `+sosCols+` FROM sos_events e JOIN users u ON u.id=e.user_id
		WHERE e.id=$1 AND EXISTS (SELECT 1 FROM trip_members m WHERE m.trip_id=e.trip_id AND m.user_id=$2 AND m.status='active')`, sosID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound()
	}
	if err != nil {
		return nil, wrap(err)
	}
	if err := s.decorate(ctx, userID, x); err != nil {
		return nil, wrap(err)
	}
	return x, nil
}

func (s *Service) decorate(ctx context.Context, viewer uuid.UUID, x *SOS) error {
	ag, err := s.aggregate(ctx, x.ID)
	if err != nil {
		return err
	}
	x.Delivery = &ag
	if x.SenderID != viewer {
		return nil
	}
	rows, err := s.db.Query(ctx, `SELECT user_id, received_at, seen_at, push_status FROM sos_recipients WHERE sos_id=$1 ORDER BY user_id`, x.ID)
	if err != nil {
		return err
	}
	x.Recipients, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Recipient, error) {
		var rc Recipient
		err := r.Scan(&rc.UserID, &rc.ReceivedAt, &rc.SeenAt, &rc.PushStatus)
		return rc, err
	})
	return err
}

// List returns the SOS of a trip, newest first.
func (s *Service) List(ctx context.Context, userID, tripID uuid.UUID, status string) ([]*SOS, error) {
	if _, _, err := s.trips.Access(ctx, tripID, userID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT `+sosCols+` FROM sos_events e JOIN users u ON u.id=e.user_id
		WHERE e.trip_id=$1 AND ($2='' OR e.status=$2) ORDER BY e.created_at DESC LIMIT 100`, tripID, status)
	if err != nil {
		return nil, wrap(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*SOS, error) { return scanSOS(r) })
	if err != nil {
		return nil, wrap(err)
	}
	for _, x := range out {
		if err := s.decorate(ctx, userID, x); err != nil {
			return nil, wrap(err)
		}
	}
	if out == nil {
		out = []*SOS{}
	}
	return out, nil
}

// Resolve closes an SOS; allowed for the sender and the trip owner (docs/design/06 §2.7).
func (s *Service) Resolve(ctx context.Context, userID, sosID uuid.UUID, to string) (*SOS, error) {
	if to != StatusResolved && to != StatusCancelled {
		return nil, apperr.BadRequest("status phải là resolved hoặc cancelled")
	}
	var ev trip.Event
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var tripID, sender uuid.UUID
		var cur string
		err := tx.QueryRow(ctx, `SELECT e.trip_id, e.user_id, e.status FROM sos_events e
			WHERE e.id=$1 AND EXISTS (SELECT 1 FROM trip_members m WHERE m.trip_id=e.trip_id AND m.user_id=$2 AND m.status='active') FOR UPDATE`,
			sosID, userID).Scan(&tripID, &sender, &cur)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if sender != userID {
			var role string
			if err := tx.QueryRow(ctx, `SELECT role FROM trip_members WHERE trip_id=$1 AND user_id=$2`, tripID, userID).Scan(&role); err != nil {
				return err
			}
			if role != "owner" {
				return apperr.Forbidden()
			}
		}
		if cur != StatusOpen {
			return apperr.Conflict("invalid_state", "SOS đã được đóng")
		}
		if _, err := tx.Exec(ctx, `UPDATE sos_events SET status=$2, resolved_by=$3, resolved_at=now() WHERE id=$1`, sosID, to, userID); err != nil {
			return err
		}
		ev, err = trip.AppendEvent(ctx, tx, tripID, &userID, trip.KindSOSResolved, map[string]any{"sos_id": sosID, "status": to, "resolved_by": userID})
		return err
	})
	if err != nil {
		return nil, wrap(err)
	}
	s.pub.PublishEvent(ctx, ev)
	return s.Get(ctx, userID, sosID)
}

// ---- WebSocket adapter (realtime.SOSHandler) ----

// WSHandler adapts the service to the WebSocket SOS messages.
type WSHandler struct{ S *Service }

func (h WSHandler) Create(ctx context.Context, userID, tripID uuid.UUID, msgID string, payload []byte) (map[string]any, error) {
	var in CreateInput
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, apperr.BadRequest("payload không hợp lệ")
	}
	if in.ClientSOSID == uuid.Nil {
		if u, err := uuid.Parse(msgID); err == nil {
			in.ClientSOSID = u
		}
	}
	r, err := h.S.Create(ctx, userID, tripID, in)
	if err != nil {
		return nil, err
	}
	return map[string]any{"sos_id": r.SOSID, "event_seq": r.EventSeq, "duplicate": r.Duplicate}, nil
}

func (h WSHandler) Receipt(ctx context.Context, userID, tripID uuid.UUID, payload []byte) error {
	var in struct {
		SOSID  uuid.UUID `json:"sos_id"`
		Status string    `json:"status"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return apperr.BadRequest("payload không hợp lệ")
	}
	return h.S.Receipt(ctx, userID, in.SOSID, in.Status)
}
