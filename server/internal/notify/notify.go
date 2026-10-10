package notify

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"motolink/internal/platform/id"
	"motolink/internal/trip"
)

// Notification types stored in notifications.type.
const (
	TypeTripStarted         = "trip_started"
	TypeTripEnded           = "trip_ended"
	TypeMemberJoined        = "member_joined"
	TypeMemberRemoved       = "member_removed"
	TypeSOS                 = "sos"
	TypeSOSResolved         = "sos_resolved"
	TypeTrackingInterrupted = "tracking_interrupted"
)

// Service creates notifications from durable events and pushes them. It implements durable.EventSideEffects.
type Service struct {
	db     *pgxpool.Pool
	pusher Pusher
	log    *slog.Logger
	sem    chan struct{}
}

func New(db *pgxpool.Pool, p Pusher, log *slog.Logger) *Service {
	if p == nil {
		p = NoopPusher{}
	}
	if log == nil {
		log = slog.Default()
	}
	return &Service{db: db, pusher: p, log: log, sem: make(chan struct{}, 8)}
}

type plan struct {
	typ        string
	recipients []uuid.UUID
	title      string
	body       string
	push       bool
	high       bool
	data       map[string]string
	payload    map[string]any
	sosID      *uuid.UUID
}

// OnEvent turns one durable event into notifications (idempotent per (user,type,event)) and pushes the new ones.
func (s *Service) OnEvent(ctx context.Context, ev trip.Event) error {
	p, err := s.plan(ctx, ev)
	if err != nil || p == nil || len(p.recipients) == 0 {
		return err
	}
	var firstErr error
	var wg sync.WaitGroup
	for _, u := range p.recipients {
		isNew, nid, err := s.insert(ctx, u, ev, p)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !isNew {
			continue // replayed event: already notified
		}
		if !p.push || !s.pusher.Enabled() {
			s.finish(ctx, nid, u, p, "skipped")
			continue
		}
		wg.Add(1)
		s.sem <- struct{}{}
		go func(u, nid uuid.UUID) {
			defer wg.Done()
			defer func() { <-s.sem }()
			s.finish(ctx, nid, u, p, s.deliver(ctx, u, nid, p))
		}(u, nid)
	}
	wg.Wait()
	return firstErr
}

func (s *Service) plan(ctx context.Context, ev trip.Event) (*plan, error) {
	var d map[string]any
	_ = json.Unmarshal(ev.Data, &d)
	str := func(k string) string { v, _ := d[k].(string); return v }
	base := map[string]string{"trip_id": ev.TripID.String(), "ref_event_id": ev.ID.String()}
	members := func() ([]uuid.UUID, error) {
		rows, err := s.db.Query(ctx, `SELECT user_id FROM trip_members WHERE trip_id=$1 AND status='active' AND ($2::uuid IS NULL OR user_id<>$2)`, ev.TripID, ev.ActorID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
	var tripName string
	_ = s.db.QueryRow(ctx, `SELECT name FROM trips WHERE id=$1`, ev.TripID).Scan(&tripName)
	with := func(t string) map[string]string {
		m := map[string]string{"type": t}
		for k, v := range base {
			m[k] = v
		}
		return m
	}
	switch ev.Kind {
	case trip.KindTripStarted, trip.KindTripEnded:
		rec, err := members()
		if err != nil {
			return nil, err
		}
		t, title := TypeTripStarted, "Chuyến đi đã bắt đầu"
		if ev.Kind == trip.KindTripEnded {
			t, title = TypeTripEnded, "Chuyến đi đã kết thúc"
		}
		return &plan{typ: t, recipients: rec, title: title, body: tripName, push: true, data: with(t),
			payload: map[string]any{"trip_name": tripName}}, nil
	case trip.KindMemberJoined:
		var owner uuid.UUID
		if err := s.db.QueryRow(ctx, `SELECT user_id FROM trip_members WHERE trip_id=$1 AND role='owner' AND status='active'`, ev.TripID).Scan(&owner); err != nil {
			return nil, nil
		}
		if ev.ActorID != nil && *ev.ActorID == owner {
			return nil, nil
		}
		return &plan{typ: TypeMemberJoined, recipients: []uuid.UUID{owner}, title: "Thành viên mới", body: str("display_name") + " đã tham gia " + tripName,
			data: with(TypeMemberJoined), payload: map[string]any{"user_id": d["user_id"], "display_name": d["display_name"]}}, nil
	case trip.KindMemberRemoved:
		u, err := uuid.Parse(str("user_id"))
		if err != nil {
			return nil, nil
		}
		return &plan{typ: TypeMemberRemoved, recipients: []uuid.UUID{u}, title: "Bạn đã bị mời ra khỏi chuyến đi", body: tripName, push: true,
			data: with(TypeMemberRemoved)}, nil
	case trip.KindSOSCreated:
		sid, err := uuid.Parse(str("sos_id"))
		if err != nil {
			return nil, nil
		}
		rows, err := s.db.Query(ctx, `SELECT user_id FROM sos_recipients WHERE sos_id=$1`, sid)
		if err != nil {
			return nil, err
		}
		rec, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return nil, err
		}
		dd := with(TypeSOS)
		dd["sos_id"], dd["kind"] = sid.String(), str("kind") // no coordinates in push payloads
		return &plan{typ: TypeSOS, recipients: rec, title: "SOS từ " + str("sender_name"), body: tripName, push: true, high: true,
			data: dd, sosID: &sid, payload: map[string]any{"sos_id": sid, "kind": str("kind"), "sender_id": d["sender_id"], "sender_name": d["sender_name"]}}, nil
	case trip.KindSOSResolved:
		rec, err := members()
		if err != nil {
			return nil, err
		}
		dd := with(TypeSOSResolved)
		dd["sos_id"] = str("sos_id")
		return &plan{typ: TypeSOSResolved, recipients: rec, title: "SOS đã được xử lý", body: tripName, push: true, data: dd,
			payload: map[string]any{"sos_id": d["sos_id"], "status": d["status"]}}, nil
	case trip.KindTrackingEnded:
		if str("end_reason") != "timeout" {
			return nil, nil
		}
		u, err := uuid.Parse(str("user_id"))
		if err != nil {
			return nil, nil
		}
		return &plan{typ: TypeTrackingInterrupted, recipients: []uuid.UUID{u}, title: "Ghi hành trình bị gián đoạn",
			body: "Hãy mở MotoLink để tiếp tục. Một số máy tự tắt ứng dụng nền.", push: true, data: with(TypeTrackingInterrupted),
			payload: map[string]any{"session_id": d["session_id"]}}, nil
	}
	return nil, nil
}

func (s *Service) insert(ctx context.Context, user uuid.UUID, ev trip.Event, p *plan) (bool, uuid.UUID, error) {
	payload := map[string]any{"title": p.title, "body": p.body}
	for k, v := range p.payload {
		payload[k] = v
	}
	raw, _ := json.Marshal(payload)
	nid := id.New()
	tag, err := s.db.Exec(ctx, `INSERT INTO notifications (id, user_id, trip_id, type, ref_event_id, payload, push_status)
		VALUES ($1,$2,$3,$4,$5,$6,'pending') ON CONFLICT (user_id, type, ref_event_id) DO NOTHING`,
		nid, user, ev.TripID, p.typ, ev.ID, raw)
	if err != nil {
		return false, uuid.Nil, err
	}
	return tag.RowsAffected() == 1, nid, nil
}

// deliver sends to every device with a token, with bounded retries for transient failures.
func (s *Service) deliver(ctx context.Context, user, nid uuid.UUID, p *plan) string {
	rows, err := s.db.Query(ctx, `SELECT fcm_token FROM devices WHERE user_id=$1 AND fcm_token IS NOT NULL`, user)
	if err != nil {
		return "failed"
	}
	tokens, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(tokens) == 0 {
		return "skipped" // no device with push: in-app only
	}
	status := "failed"
	data := map[string]string{}
	for k, v := range p.data {
		data[k] = v
	}
	data["notification_id"] = nid.String()
	for _, tk := range tokens {
		var err error
		for attempt := 0; attempt < 5; attempt++ {
			err = s.pusher.Send(ctx, tk, Push{Title: p.title, Body: p.body, Data: data, High: p.high})
			if err == nil || !errors.Is(err, ErrTransient) {
				break
			}
			select {
			case <-ctx.Done():
				return "failed"
			case <-time.After(time.Second << attempt):
			}
		}
		switch {
		case err == nil:
			status = "sent"
		case errors.Is(err, ErrUnregistered):
			_, _ = s.db.Exec(ctx, `UPDATE devices SET fcm_token=NULL, fcm_updated_at=now() WHERE fcm_token=$1`, tk)
		default:
			s.log.Warn("push failed", "type", p.typ, "err", err)
		}
	}
	return status
}

func (s *Service) finish(ctx context.Context, nid, user uuid.UUID, p *plan, status string) {
	_, _ = s.db.Exec(ctx, `UPDATE notifications SET push_status=$2, push_attempts=push_attempts+1 WHERE id=$1`, nid, status)
	if p.sosID == nil {
		return
	}
	rs := status
	switch status {
	case "skipped":
		rs = "no_token"
	}
	_, _ = s.db.Exec(ctx, `UPDATE sos_recipients SET push_status=$3, delivered_push_at = CASE WHEN $3='sent' THEN now() ELSE delivered_push_at END
		WHERE sos_id=$1 AND user_id=$2`, *p.sosID, user, rs)
}

// ---- inbox ----

type Item struct {
	ID         uuid.UUID       `json:"id"`
	TripID     *uuid.UUID      `json:"trip_id"`
	Type       string          `json:"type"`
	RefEventID *uuid.UUID      `json:"ref_event_id"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
	ReadAt     *time.Time      `json:"read_at"`
}

// List returns the newest notifications first; cursor is the created_at (RFC3339Nano) of the last item seen.
func (s *Service) List(ctx context.Context, user uuid.UUID, cursor string, limit int) ([]Item, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	before := time.Now().Add(time.Hour)
	if cursor != "" {
		t, err := time.Parse(time.RFC3339Nano, cursor)
		if err != nil {
			return nil, "", errors.New("bad cursor")
		}
		before = t
	}
	rows, err := s.db.Query(ctx, `SELECT id, trip_id, type, ref_event_id, payload, created_at, read_at FROM notifications
		WHERE user_id=$1 AND created_at < $2 ORDER BY created_at DESC LIMIT $3`, user, before, limit+1)
	if err != nil {
		return nil, "", err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Item, error) {
		var it Item
		err := r.Scan(&it.ID, &it.TripID, &it.Type, &it.RefEventID, &it.Payload, &it.CreatedAt, &it.ReadAt)
		return it, err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].CreatedAt.Format(time.RFC3339Nano)
	}
	if items == nil {
		items = []Item{}
	}
	return items, next, nil
}

// MarkRead marks the given notifications (or all) as read.
func (s *Service) MarkRead(ctx context.Context, user uuid.UUID, ids []uuid.UUID, all bool) error {
	if all {
		_, err := s.db.Exec(ctx, `UPDATE notifications SET read_at=now() WHERE user_id=$1 AND read_at IS NULL`, user)
		return err
	}
	_, err := s.db.Exec(ctx, `UPDATE notifications SET read_at=now() WHERE user_id=$1 AND id=ANY($2) AND read_at IS NULL`, user, ids)
	return err
}
