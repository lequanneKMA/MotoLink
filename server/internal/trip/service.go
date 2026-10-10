package trip

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"motolink/internal/platform/apperr"
	"motolink/internal/platform/id"
)

// EventRetention bounds how far back clients may replay events (docs/design/07 §6).
const EventRetention = 7 * 24 * time.Hour

type Service struct {
	db   *pgxpool.Pool
	pub  EventPublisher
	live LiveState
	now  func() time.Time
}

func NewService(db *pgxpool.Pool, pub EventPublisher) *Service {
	if pub == nil {
		pub = NoopPublisher{}
	}
	return &Service{db: db, pub: pub, now: time.Now}
}

// SetPublisher wires the realtime hub after construction (avoids an init cycle).
func (s *Service) SetPublisher(p EventPublisher) { s.pub = p }

const tripCols = `id, owner_id, name, status, history_visibility, max_members, event_seq, started_at, ended_at, created_at`

func scanTrip(r pgx.Row) (*Trip, error) {
	t := &Trip{}
	err := r.Scan(&t.ID, &t.OwnerID, &t.Name, &t.Status, &t.HistoryVisibility, &t.MaxMembers, &t.EventSeq, &t.StartedAt, &t.EndedAt, &t.CreatedAt)
	return t, err
}

// ---- authorisation (single entry point for REST and WS, docs/design/11 §3) ----

// Access loads the trip and the caller's membership. A caller who is not an active
// member gets NotFound, so trip ids cannot be probed (docs/design/06 §1.4).
func (s *Service) Access(ctx context.Context, tripID, userID uuid.UUID) (*Trip, *Membership, error) {
	return access(ctx, s.db, tripID, userID, false)
}

type querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func access(ctx context.Context, q querier, tripID, userID uuid.UUID, lock bool) (*Trip, *Membership, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	t, err := scanTrip(q.QueryRow(ctx, `SELECT `+tripCols+` FROM trips WHERE id=$1`+suffix, tripID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, apperr.NotFound()
	}
	if err != nil {
		return nil, nil, wrap(err)
	}
	m := &Membership{}
	err = q.QueryRow(ctx, `SELECT trip_id,user_id,role,status,sharing_state,share_history,coalesce(color,''),joined_at,left_at
		FROM trip_members WHERE trip_id=$1 AND user_id=$2`, tripID, userID).
		Scan(&m.TripID, &m.UserID, &m.Role, &m.Status, &m.SharingState, &m.ShareHistory, &m.Color, &m.JoinedAt, &m.LeftAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && m.Status != MemberActive) {
		return nil, nil, apperr.NotFound()
	}
	if err != nil {
		return nil, nil, wrap(err)
	}
	return t, m, nil
}

// RequireMember returns the trip for an active member.
func (s *Service) RequireMember(ctx context.Context, tripID, userID uuid.UUID) (*Trip, *Membership, error) {
	return s.Access(ctx, tripID, userID)
}

// RequireOwner is RequireMember plus the owner role (403 for a plain member).
func (s *Service) RequireOwner(ctx context.Context, tripID, userID uuid.UUID) (*Trip, *Membership, error) {
	t, m, err := s.Access(ctx, tripID, userID)
	if err != nil {
		return nil, nil, err
	}
	if m.Role != RoleOwner {
		return nil, nil, apperr.Forbidden()
	}
	return t, m, nil
}

// ---- trip lifecycle ----

func validName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n >= 1 && n <= 80
}

type CreateInput struct {
	Name              string `json:"name"`
	HistoryVisibility string `json:"history_visibility"`
}

// Create makes a draft trip with the caller as owner (T3).
func (s *Service) Create(ctx context.Context, owner uuid.UUID, in CreateInput) (*Trip, error) {
	name, ok := validName(in.Name)
	if !ok {
		return nil, apperr.Validation(apperr.FieldError{Field: "name", Code: "invalid_length"})
	}
	vis := in.HistoryVisibility
	if vis == "" {
		vis = "members"
	}
	if vis != "members" && vis != "self_only" {
		return nil, apperr.Validation(apperr.FieldError{Field: "history_visibility", Code: "invalid"})
	}
	tid := id.New()
	var t *Trip
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		t, err = scanTrip(tx.QueryRow(ctx,
			`INSERT INTO trips (id, owner_id, name, history_visibility) VALUES ($1,$2,$3,$4) RETURNING `+tripCols,
			tid, owner, name, vis))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO trip_members (trip_id,user_id,role,status,color) VALUES ($1,$2,'owner','active',$3)`,
			tid, owner, colorFor(0)); err != nil {
			return err
		}
		ev, err := AppendEvent(ctx, tx, tid, &owner, KindTripCreated, map[string]any{"name": name})
		if err != nil {
			return err
		}
		t.EventSeq = ev.Seq
		return nil
	})
	if err != nil {
		return nil, wrap(err)
	}
	return t, nil
}

type TripSummary struct {
	Trip
	MyRole      string `json:"my_role"`
	MemberCount int    `json:"member_count"`
}

// List returns trips where the caller is an active member, newest first.
func (s *Service) List(ctx context.Context, userID uuid.UUID, status string, includeArchived bool) ([]TripSummary, error) {
	q := `SELECT t.id,t.owner_id,t.name,t.status,t.history_visibility,t.max_members,t.event_seq,t.started_at,t.ended_at,t.created_at,
	             m.role, (SELECT count(*) FROM trip_members x WHERE x.trip_id=t.id AND x.status='active')
	      FROM trips t JOIN trip_members m ON m.trip_id=t.id AND m.user_id=$1 AND m.status='active'
	      WHERE ($2 = '' OR t.status = $2) AND ($3 OR t.status <> 'archived')
	      ORDER BY t.created_at DESC LIMIT 200`
	rows, err := s.db.Query(ctx, q, userID, status, includeArchived || status == StatusArchived)
	if err != nil {
		return nil, wrap(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TripSummary, error) {
		var x TripSummary
		err := r.Scan(&x.ID, &x.OwnerID, &x.Name, &x.Status, &x.HistoryVisibility, &x.MaxMembers, &x.EventSeq,
			&x.StartedAt, &x.EndedAt, &x.CreatedAt, &x.MyRole, &x.MemberCount)
		return x, err
	})
	if err != nil {
		return nil, wrap(err)
	}
	if out == nil {
		out = []TripSummary{}
	}
	return out, nil
}

type UpdateInput struct {
	Name              *string `json:"name"`
	HistoryVisibility *string `json:"history_visibility"`
}

func (s *Service) Update(ctx context.Context, tripID, userID uuid.UUID, in UpdateInput) (*Trip, error) {
	if _, _, err := s.RequireOwner(ctx, tripID, userID); err != nil {
		return nil, err
	}
	var name *string
	if in.Name != nil {
		n, ok := validName(*in.Name)
		if !ok {
			return nil, apperr.Validation(apperr.FieldError{Field: "name", Code: "invalid_length"})
		}
		name = &n
	}
	if in.HistoryVisibility != nil && *in.HistoryVisibility != "members" && *in.HistoryVisibility != "self_only" {
		return nil, apperr.Validation(apperr.FieldError{Field: "history_visibility", Code: "invalid"})
	}
	t, err := scanTrip(s.db.QueryRow(ctx, `
		UPDATE trips SET name=COALESCE($2,name), history_visibility=COALESCE($3,history_visibility), updated_at=now()
		WHERE id=$1 AND status <> 'archived' RETURNING `+tripCols, tripID, name, in.HistoryVisibility))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.Conflict("invalid_state", "Chuyến đi đã lưu trữ")
	}
	if err != nil {
		return nil, wrap(err)
	}
	return t, nil
}

func invalidState() *apperr.Error {
	return apperr.Conflict("invalid_state", "Trạng thái chuyến đi không cho phép thao tác này")
}

// Start: draft -> active (owner).
func (s *Service) Start(ctx context.Context, tripID, userID uuid.UUID) (*Trip, error) {
	return s.transition(ctx, tripID, userID, func(ctx context.Context, tx pgx.Tx, t *Trip) ([]Event, error) {
		tag, err := tx.Exec(ctx, `UPDATE trips SET status='active', started_at=now(), updated_at=now() WHERE id=$1 AND status='draft'`, tripID)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, invalidState()
		}
		ev, err := AppendEvent(ctx, tx, tripID, &userID, KindTripStarted, map[string]any{})
		return []Event{ev}, err
	})
}

// End: draft|active -> ended (owner). Closes every open tracking session.
func (s *Service) End(ctx context.Context, tripID, userID uuid.UUID) (*Trip, error) {
	t, err := s.transition(ctx, tripID, userID, func(ctx context.Context, tx pgx.Tx, t *Trip) ([]Event, error) {
		return endTx(ctx, tx, tripID, &userID, "trip_ended")
	})
	if err == nil {
		s.pub.TripEnded(ctx, tripID)
	}
	return t, err
}

// endTx ends a trip and its open sessions inside tx. Shared with auto-end and owner departure.
func endTx(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, actor *uuid.UUID, reason string) ([]Event, error) {
	tag, err := tx.Exec(ctx, `UPDATE trips SET status='ended', ended_at=now(), updated_at=now() WHERE id=$1 AND status IN ('draft','active')`, tripID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, invalidState()
	}
	var evs []Event
	ses, err := EndOpenSessionsTx(ctx, tx, tripID, nil, "trip_ended")
	if err != nil {
		return nil, err
	}
	for _, se := range ses {
		ev, err := AppendEvent(ctx, tx, tripID, actor, KindTrackingEnded, se)
		if err != nil {
			return nil, err
		}
		evs = append(evs, ev)
	}
	ev, err := AppendEvent(ctx, tx, tripID, actor, KindTripEnded, map[string]any{"reason": reason})
	if err != nil {
		return nil, err
	}
	return append(evs, ev), nil
}

// Archive: ended -> archived (owner).
func (s *Service) Archive(ctx context.Context, tripID, userID uuid.UUID) (*Trip, error) {
	return s.transition(ctx, tripID, userID, func(ctx context.Context, tx pgx.Tx, t *Trip) ([]Event, error) {
		tag, err := tx.Exec(ctx, `UPDATE trips SET status='archived', archived_at=now(), updated_at=now() WHERE id=$1 AND status='ended'`, tripID)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, invalidState()
		}
		ev, err := AppendEvent(ctx, tx, tripID, &userID, KindTripArchived, map[string]any{})
		return []Event{ev}, err
	})
}

// transition runs an owner-only state change in one transaction and publishes the events after commit.
func (s *Service) transition(ctx context.Context, tripID, userID uuid.UUID,
	fn func(context.Context, pgx.Tx, *Trip) ([]Event, error)) (*Trip, error) {
	var (
		out *Trip
		evs []Event
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		t, m, err := access(ctx, tx, tripID, userID, true)
		if err != nil {
			return err
		}
		if m.Role != RoleOwner {
			return apperr.Forbidden()
		}
		if evs, err = fn(ctx, tx, t); err != nil {
			return err
		}
		out, err = scanTrip(tx.QueryRow(ctx, `SELECT `+tripCols+` FROM trips WHERE id=$1`, tripID))
		return err
	})
	if err != nil {
		return nil, wrap(err)
	}
	s.publish(ctx, evs)
	return out, nil
}

func (s *Service) publish(ctx context.Context, evs []Event) {
	for _, ev := range evs {
		s.pub.PublishEvent(ctx, ev)
	}
}

// AutoEndIdle ends active trips that have had no open tracking session for idle (docs/design/05 §5). Returns the count.
func (s *Service) AutoEndIdle(ctx context.Context, idle time.Duration) (int, error) {
	rows, err := s.db.Query(ctx, `
		SELECT t.id, t.owner_id FROM trips t
		WHERE t.status='active'
		  AND coalesce(t.started_at, t.created_at) < now() - make_interval(secs => $1)
		  AND NOT EXISTS (SELECT 1 FROM tracking_sessions s WHERE s.trip_id=t.id AND s.status IN ('active','paused'))
		  AND NOT EXISTS (SELECT 1 FROM tracking_sessions s WHERE s.trip_id=t.id AND coalesce(s.ended_at, s.started_at) > now() - make_interval(secs => $1))`,
		idle.Seconds())
	if err != nil {
		return 0, wrap(err)
	}
	type row struct{ id, owner uuid.UUID }
	cands, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) { var x row; err := r.Scan(&x.id, &x.owner); return x, err })
	if err != nil {
		return 0, wrap(err)
	}
	n := 0
	for _, c := range cands {
		var evs []Event
		err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT 1 FROM trips WHERE id=$1 FOR UPDATE`, c.id); err != nil {
				return err
			}
			var e error
			evs, e = endTx(ctx, tx, c.id, nil, "idle_timeout")
			return e
		})
		if err != nil {
			if e, ok := apperr.As(err); ok && e.Code == "invalid_state" {
				continue // raced with an owner action
			}
			return n, wrap(err)
		}
		s.publish(ctx, evs)
		s.pub.TripEnded(ctx, c.id)
		n++
	}
	return n, nil
}

// ---- reads for snapshot / catch-up ----

type EventPage struct {
	Items        []Event `json:"items"`
	HeadEventSeq int64   `json:"head_event_seq"`
	// Complete: no more events after this page (up to the head). More pages exist when false and !Expired.
	Complete bool `json:"complete"`
	// Expired: events between the cursor and the first returned one are gone (retention); the client must resync from a snapshot.
	Expired bool `json:"-"`
}

// Events returns durable events after seq (max 500, within retention). If the caller's cursor is
// older than retention and events were skipped, Complete is false and they must resync (410 at REST level).
func (s *Service) Events(ctx context.Context, tripID, userID uuid.UUID, after int64, limit int) (*EventPage, error) {
	t, _, err := s.Access(ctx, tripID, userID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	if after < 0 {
		after = 0
	}
	items, err := ListEvents(ctx, s.db, tripID, after, limit+1, s.now().Add(-EventRetention))
	if err != nil {
		return nil, wrap(err)
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	// Gap detection: the first returned seq must be after+1, else older events expired.
	expired := (len(items) > 0 && items[0].Seq != after+1) || (len(items) == 0 && t.EventSeq > after)
	if items == nil {
		items = []Event{}
	}
	return &EventPage{Items: items, HeadEventSeq: t.EventSeq, Complete: !more && !expired, Expired: expired}, nil
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
	return apperr.Internal(fmt.Errorf("trip: %w", err))
}
