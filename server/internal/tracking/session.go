package tracking

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"motolink/internal/platform/apperr"
	"motolink/internal/platform/id"
	"motolink/internal/trip"
)

const (
	StatusActive = "active"
	StatusPaused = "paused"
	StatusEnded  = "ended"
)

type Session struct {
	ID            uuid.UUID  `json:"id"`
	TripID        uuid.UUID  `json:"trip_id"`
	UserID        uuid.UUID  `json:"user_id"`
	DeviceID      *uuid.UUID `json:"device_id,omitempty"`
	Status        string     `json:"status"`
	EndReason     *string    `json:"end_reason,omitempty"`
	SessionMonth  string     `json:"session_month"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	LastSeq       int32      `json:"last_seq"`
	PointCount    *int       `json:"point_count,omitempty"`
	DistanceM     *float64   `json:"distance_m,omitempty"`
	DurationS     *int       `json:"duration_s,omitempty"`
	RoutePolyline *string    `json:"route_polyline,omitempty"`
}

const sessionCols = `id,trip_id,user_id,device_id,status,end_reason,to_char(session_month,'YYYY-MM-DD'),started_at,ended_at,last_seq,point_count,distance_m,duration_s,route_polyline`

func scanSession(r pgx.Row) (*Session, error) {
	s := &Session{}
	err := r.Scan(&s.ID, &s.TripID, &s.UserID, &s.DeviceID, &s.Status, &s.EndReason, &s.SessionMonth, &s.StartedAt, &s.EndedAt,
		&s.LastSeq, &s.PointCount, &s.DistanceM, &s.DurationS, &s.RoutePolyline)
	return s, err
}

// ConsentChecker answers whether a user currently consents to location processing (docs/design/11 §5.1).
type ConsentChecker interface {
	HasConsent(ctx context.Context, userID uuid.UUID, kind string) (bool, error)
}

type Service struct {
	db      *pgxpool.Pool
	trips   *trip.Service
	consent ConsentChecker
	pub     trip.EventPublisher
	now     func() time.Time
}

func NewService(db *pgxpool.Pool, trips *trip.Service, consent ConsentChecker, pub trip.EventPublisher) *Service {
	if pub == nil {
		pub = trip.NoopPublisher{}
	}
	return &Service{db: db, trips: trips, consent: consent, pub: pub, now: time.Now}
}

func (s *Service) SetPublisher(p trip.EventPublisher) { s.pub = p }

// Start creates a tracking session (T7). session_id is server-issued so that ordering across
// sessions of one user is trustworthy (ADR-004).
func (s *Service) Start(ctx context.Context, userID, tripID uuid.UUID, deviceID *uuid.UUID) (*Session, error) {
	t, _, err := s.trips.Access(ctx, tripID, userID)
	if err != nil {
		return nil, err
	}
	if t.Status != trip.StatusActive {
		return nil, apperr.Conflict("trip_not_active", "Chuyến đi chưa bắt đầu hoặc đã kết thúc")
	}
	ok, err := s.consent.HasConsent(ctx, userID, "location_processing")
	if err != nil {
		return nil, wrap(err)
	}
	if !ok {
		return nil, apperr.ForbiddenCode("consent_required", "Cần đồng ý xử lý dữ liệu vị trí")
	}
	sid := id.New()
	var out *Session
	var ev trip.Event
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// serialise with trip end: lock the trip row and re-check
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM trips WHERE id=$1 FOR SHARE`, tripID).Scan(&status); err != nil {
			return err
		}
		if status != trip.StatusActive {
			return apperr.Conflict("trip_not_active", "Chuyến đi chưa bắt đầu hoặc đã kết thúc")
		}
		var e error
		out, e = scanSession(tx.QueryRow(ctx, `
			INSERT INTO tracking_sessions (id,trip_id,user_id,device_id,session_month)
			VALUES ($1,$2,$3,$4, date_trunc('month', now() AT TIME ZONE 'UTC')::date) RETURNING `+sessionCols,
			sid, tripID, userID, deviceID))
		if e != nil {
			return e
		}
		ev, e = trip.AppendEvent(ctx, tx, tripID, &userID, trip.KindTrackingStarted,
			map[string]any{"session_id": sid, "user_id": userID})
		return e
	})
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" && pe.ConstraintName == "tracking_sessions_one_open_uk" {
		cur, e2 := scanSession(s.db.QueryRow(ctx, `SELECT `+sessionCols+` FROM tracking_sessions WHERE trip_id=$1 AND user_id=$2 AND status IN ('active','paused')`, tripID, userID))
		ae := apperr.Conflict("session_already_open", "Bạn đã có phiên ghi đang mở trong chuyến đi này")
		if e2 == nil {
			ae.Extra = map[string]any{"session": cur}
		}
		return nil, ae
	}
	if err != nil {
		return nil, wrap(err)
	}
	s.pub.PublishEvent(ctx, ev)
	return out, nil
}

// Get returns a session the viewer may see (own, or a member's when history is shared).
func (s *Service) Get(ctx context.Context, viewer, sessionID uuid.UUID) (*Session, error) {
	ses, err := scanSession(s.db.QueryRow(ctx, `SELECT `+sessionCols+` FROM tracking_sessions WHERE id=$1`, sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound()
	}
	if err != nil {
		return nil, wrap(err)
	}
	if err := s.canView(ctx, viewer, ses); err != nil {
		return nil, err
	}
	return ses, nil
}

// canView: owner always; otherwise an active member of the trip when the trip shares history and the owner allows it.
func (s *Service) canView(ctx context.Context, viewer uuid.UUID, ses *Session) error {
	if ses.UserID == viewer {
		return nil
	}
	t, _, err := s.trips.Access(ctx, ses.TripID, viewer)
	if err != nil {
		return err // 404 for non-members
	}
	if t.HistoryVisibility != "members" {
		return apperr.NotFound()
	}
	var share bool
	err = s.db.QueryRow(ctx, `SELECT share_history FROM trip_members WHERE trip_id=$1 AND user_id=$2`, ses.TripID, ses.UserID).Scan(&share)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !share) {
		return apperr.NotFound()
	}
	return wrap(err)
}

// ListForTrip lists sessions of a trip visible to viewer, newest first. userID optionally filters.
func (s *Service) ListForTrip(ctx context.Context, viewer, tripID uuid.UUID, userID *uuid.UUID) ([]Session, error) {
	t, _, err := s.trips.Access(ctx, tripID, viewer)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT `+prefixCols("s")+` FROM tracking_sessions s
		LEFT JOIN trip_members m ON m.trip_id=s.trip_id AND m.user_id=s.user_id
		WHERE s.trip_id=$1 AND ($2::uuid IS NULL OR s.user_id=$2)
		  AND (s.user_id=$3 OR ($4 AND coalesce(m.share_history,false)))
		ORDER BY s.started_at DESC LIMIT 200`, tripID, userID, viewer, t.HistoryVisibility == "members")
	if err != nil {
		return nil, wrap(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Session, error) {
		ses, err := scanSession(r)
		return *ses, err
	})
	if err != nil {
		return nil, wrap(err)
	}
	if out == nil {
		out = []Session{}
	}
	return out, nil
}

func prefixCols(p string) string {
	return fmt.Sprintf(`%[1]s.id,%[1]s.trip_id,%[1]s.user_id,%[1]s.device_id,%[1]s.status,%[1]s.end_reason,to_char(%[1]s.session_month,'YYYY-MM-DD'),%[1]s.started_at,%[1]s.ended_at,%[1]s.last_seq,%[1]s.point_count,%[1]s.distance_m,%[1]s.duration_s,%[1]s.route_polyline`, p)
}

// LookupOpen returns an active/paused session by id regardless of viewer (used by ingest, which has
// already authenticated the connection). ErrNoRows-style NotFound if missing or ended.
func (s *Service) LookupOpen(ctx context.Context, sessionID uuid.UUID) (*Session, error) {
	ses, err := scanSession(s.db.QueryRow(ctx, `SELECT `+sessionCols+` FROM tracking_sessions WHERE id=$1`, sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound()
	}
	if err != nil {
		return nil, wrap(err)
	}
	return ses, nil
}

// Pause / Resume / End are owner-only and conditional on the current status.
func (s *Service) Pause(ctx context.Context, userID, sessionID uuid.UUID) (*Session, error) {
	return s.change(ctx, userID, sessionID, StatusPaused, trip.KindTrackingPaused, "AND status='active'", "")
}

func (s *Service) Resume(ctx context.Context, userID, sessionID uuid.UUID) (*Session, error) {
	return s.change(ctx, userID, sessionID, StatusActive, trip.KindTrackingResumed, "AND status='paused'", "")
}

func (s *Service) End(ctx context.Context, userID, sessionID uuid.UUID) (*Session, error) {
	return s.change(ctx, userID, sessionID, StatusEnded, trip.KindTrackingEnded, "AND status IN ('active','paused')", "user")
}

func (s *Service) change(ctx context.Context, userID, sessionID uuid.UUID, to, kind, cond, endReason string) (*Session, error) {
	var (
		out *Session
		ev  trip.Event
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var owner, tripID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT user_id, trip_id FROM tracking_sessions WHERE id=$1 FOR UPDATE`, sessionID).Scan(&owner, &tripID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.NotFound()
			}
			return err
		}
		if owner != userID {
			return apperr.NotFound()
		}
		q := `UPDATE tracking_sessions SET status=$2`
		args := []any{sessionID, to}
		if to == StatusEnded {
			q += `, ended_at=now(), end_reason=$3`
			args = append(args, endReason)
		}
		q += ` WHERE id=$1 ` + cond + ` RETURNING ` + sessionCols
		ses, err := scanSession(tx.QueryRow(ctx, q, args...))
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict("invalid_state", "Trạng thái phiên không cho phép thao tác này")
		}
		if err != nil {
			return err
		}
		out = ses
		data := map[string]any{"session_id": sessionID, "user_id": userID}
		if to == StatusEnded {
			data["end_reason"] = endReason
		}
		ev, err = trip.AppendEvent(ctx, tx, tripID, &userID, kind, data)
		return err
	})
	if err != nil {
		return nil, wrap(err)
	}
	s.pub.PublishEvent(ctx, ev)
	return out, nil
}

// MarkTimedOut ends sessions whose owner has not been seen for idle (docs/design/05 §5: timeout).
func (s *Service) MarkTimedOut(ctx context.Context, idle time.Duration) (int, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, trip_id, user_id FROM tracking_sessions
		WHERE status IN ('active','paused') AND coalesce(last_seen_at, started_at) < now() - make_interval(secs => $1)`, idle.Seconds())
	if err != nil {
		return 0, wrap(err)
	}
	type c struct{ id, trip, user uuid.UUID }
	cands, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (c, error) { var x c; err := r.Scan(&x.id, &x.trip, &x.user); return x, err })
	if err != nil {
		return 0, wrap(err)
	}
	n := 0
	for _, x := range cands {
		var ev trip.Event
		err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `UPDATE tracking_sessions SET status='ended', end_reason='timeout', ended_at=now() WHERE id=$1 AND status IN ('active','paused')`, x.id)
			if err != nil || tag.RowsAffected() == 0 {
				return err
			}
			ev, err = trip.AppendEvent(ctx, tx, x.trip, nil, trip.KindTrackingEnded,
				map[string]any{"session_id": x.id, "user_id": x.user, "end_reason": "timeout"})
			return err
		})
		if err != nil {
			return n, wrap(err)
		}
		if ev.Seq != 0 {
			s.pub.PublishEvent(ctx, ev)
			n++
		}
	}
	return n, nil
}

// Touch records liveness of a session (called lazily by ingest, not per point).
func (s *Service) Touch(ctx context.Context, sessionID uuid.UUID, lastSeq int64) error {
	_, err := s.db.Exec(ctx, `UPDATE tracking_sessions SET last_seen_at=now(), last_seq=greatest(last_seq,$2::int) WHERE id=$1`, sessionID, lastSeq)
	return err
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
	return apperr.Internal(fmt.Errorf("tracking: %w", err))
}

// EndAllOpenForUser ends every open session of a user (consent withdrawn, account deletion).
func (s *Service) EndAllOpenForUser(ctx context.Context, userID uuid.UUID, reason string) error {
	rows, err := s.db.Query(ctx, `SELECT id FROM tracking_sessions WHERE user_id=$1 AND status IN ('active','paused')`, userID)
	if err != nil {
		return wrap(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return wrap(err)
	}
	for _, sid := range ids {
		if _, err := s.change(ctx, userID, sid, StatusEnded, trip.KindTrackingEnded, "AND status IN ('active','paused')", reason); err != nil {
			if ae, ok := apperr.As(err); ok && ae.Code == "invalid_state" {
				continue
			}
			return err
		}
	}
	return nil
}
