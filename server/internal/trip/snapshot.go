package trip

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// LiveLocation is the latest position of a member (Redis/Hub), docs/design/07 §3.2.
type LiveLocation struct {
	SessionID string   `json:"session_id"`
	Seq       int64    `json:"seq"`
	Lat       float64  `json:"lat"`
	Lng       float64  `json:"lng"`
	AccuracyM *float32 `json:"accuracy_m,omitempty"`
	SpeedMps  *float32 `json:"speed_mps,omitempty"`
	Heading   *float32 `json:"heading_deg,omitempty"`
	ServerTs  int64    `json:"server_ts"`
}

// LiveState is implemented by the realtime layer (Redis + hub memory).
type LiveState interface {
	// Locations returns the latest known location per user for the given users.
	Locations(ctx context.Context, tripID uuid.UUID, users []uuid.UUID) (map[uuid.UUID]LiveLocation, error)
	// Online returns which of the users currently have a live connection.
	Online(ctx context.Context, tripID uuid.UUID, users []uuid.UUID) (map[uuid.UUID]bool, error)
}

func (s *Service) SetLive(l LiveState) { s.live = l }

type SnapshotMember struct {
	UserID       uuid.UUID     `json:"user_id"`
	DisplayName  string        `json:"display_name"`
	Color        string        `json:"color"`
	Role         string        `json:"role"`
	SharingState string        `json:"sharing_state"`
	Presence     string        `json:"presence"`
	Location     *LiveLocation `json:"location"`
}

type OpenSession struct {
	ID        uuid.UUID `json:"id"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	LastSeq   int32     `json:"last_seq"`
}

type Snapshot struct {
	Trip          *Trip            `json:"trip"`
	Members       []SnapshotMember `json:"members"`
	HeadEventSeq  int64            `json:"head_event_seq"`
	MyOpenSession *OpenSession     `json:"my_open_session"`
	Degraded      bool             `json:"degraded,omitempty"`
}

// Snapshot builds the state a client needs to (re)sync (GET /trips/{id}/snapshot, docs/design/06).
// Members who pause sharing never expose a location, whatever Redis holds.
func (s *Service) Snapshot(ctx context.Context, tripID, userID uuid.UUID) (*Snapshot, error) {
	t, _, err := s.Access(ctx, tripID, userID)
	if err != nil {
		return nil, err
	}
	members, err := s.members(ctx, tripID, false)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		if m.SharingState == SharingOn {
			ids = append(ids, m.UserID)
		}
	}
	var locs map[uuid.UUID]LiveLocation
	var online map[uuid.UUID]bool
	degraded := false
	if s.live != nil {
		all := make([]uuid.UUID, 0, len(members))
		for _, m := range members {
			all = append(all, m.UserID)
		}
		if locs, err = s.live.Locations(ctx, tripID, ids); err != nil {
			degraded = true
		}
		if online, err = s.live.Online(ctx, tripID, all); err != nil {
			degraded = true
		}
	}
	snap := &Snapshot{Trip: t, HeadEventSeq: t.EventSeq, Degraded: degraded}
	for _, m := range members {
		sm := SnapshotMember{UserID: m.UserID, DisplayName: m.DisplayName, Color: m.Color, Role: m.Role,
			SharingState: m.SharingState, Presence: "offline"}
		if online[m.UserID] {
			sm.Presence = "online"
		}
		if l, ok := locs[m.UserID]; ok && m.SharingState == SharingOn {
			l := l
			sm.Location = &l
		}
		snap.Members = append(snap.Members, sm)
	}
	if snap.Members == nil {
		snap.Members = []SnapshotMember{}
	}
	snap.MyOpenSession, err = s.openSession(ctx, tripID, userID)
	if err != nil {
		return nil, err
	}
	return snap, nil
}

func (s *Service) openSession(ctx context.Context, tripID, userID uuid.UUID) (*OpenSession, error) {
	o := &OpenSession{}
	err := s.db.QueryRow(ctx, `SELECT id,status,started_at,last_seq FROM tracking_sessions WHERE trip_id=$1 AND user_id=$2 AND status IN ('active','paused')`,
		tripID, userID).Scan(&o.ID, &o.Status, &o.StartedAt, &o.LastSeq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return o, wrap(err)
}

type BootstrapTrip struct {
	Trip          Trip         `json:"trip"`
	MyMembership  Membership   `json:"my_membership"`
	HeadEventSeq  int64        `json:"head_event_seq"`
	MyOpenSession *OpenSession `json:"my_open_session"`
}

// Bootstrap lists everything a freshly installed/long-offline client needs (GET /sync/bootstrap).
func (s *Service) Bootstrap(ctx context.Context, userID uuid.UUID) ([]BootstrapTrip, error) {
	rows, err := s.db.Query(ctx, `
		SELECT t.id FROM trips t JOIN trip_members m ON m.trip_id=t.id AND m.user_id=$1 AND m.status='active'
		WHERE t.status <> 'archived' ORDER BY t.created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, wrap(err)
	}
	idsList, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, wrap(err)
	}
	out := make([]BootstrapTrip, 0, len(idsList))
	for _, tid := range idsList {
		t, m, err := s.Access(ctx, tid, userID)
		if err != nil {
			continue // lost access between the two queries
		}
		os, err := s.openSession(ctx, tid, userID)
		if err != nil {
			return nil, err
		}
		out = append(out, BootstrapTrip{Trip: *t, MyMembership: *m, HeadEventSeq: t.EventSeq, MyOpenSession: os})
	}
	return out, nil
}
