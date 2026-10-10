// Package trip owns trips, membership, invitations and the durable trip event log
// (docs/design/05 §4–5, ADR-008, ADR-012).
package trip

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	StatusDraft    = "draft"
	StatusActive   = "active"
	StatusEnded    = "ended"
	StatusArchived = "archived"

	RoleOwner  = "owner"
	RoleMember = "member"

	MemberActive  = "active"
	MemberLeft    = "left"
	MemberRemoved = "removed"

	SharingOn     = "sharing"
	SharingPaused = "paused"
)

// Event kinds (docs/design/07 §3.5). Clients ignore kinds they do not know.
const (
	KindTripCreated       = "trip_created"
	KindTripStarted       = "trip_started"
	KindTripEnded         = "trip_ended"
	KindTripArchived      = "trip_archived"
	KindMemberJoined      = "member_joined"
	KindMemberLeft        = "member_left"
	KindMemberRemoved     = "member_removed"
	KindSharingChanged    = "member_sharing_changed"
	KindOwnershipTransfer = "ownership_transferred"
	KindTrackingStarted   = "tracking_started"
	KindTrackingPaused    = "tracking_paused"
	KindTrackingResumed   = "tracking_resumed"
	KindTrackingEnded     = "tracking_ended"
	KindSOSCreated        = "sos_created"
	KindSOSResolved       = "sos_resolved"
)

type Trip struct {
	ID                uuid.UUID  `json:"id"`
	OwnerID           uuid.UUID  `json:"owner_id"`
	Name              string     `json:"name"`
	Status            string     `json:"status"`
	HistoryVisibility string     `json:"history_visibility"`
	MaxMembers        int        `json:"max_members"`
	EventSeq          int64      `json:"head_event_seq"`
	StartedAt         *time.Time `json:"started_at"`
	EndedAt           *time.Time `json:"ended_at"`
	CreatedAt         time.Time  `json:"created_at"`
}

type Membership struct {
	TripID       uuid.UUID  `json:"trip_id"`
	UserID       uuid.UUID  `json:"user_id"`
	Role         string     `json:"role"`
	Status       string     `json:"status"`
	SharingState string     `json:"sharing_state"`
	ShareHistory bool       `json:"share_history"`
	Color        string     `json:"color"`
	JoinedAt     time.Time  `json:"joined_at"`
	LeftAt       *time.Time `json:"left_at,omitempty"`
}

// Member is a membership joined with the public profile.
type Member struct {
	Membership
	DisplayName string `json:"display_name"`
}

// Event is one row of the durable per-trip log; Seq is gap-free per trip.
type Event struct {
	ID        uuid.UUID       `json:"id"`
	TripID    uuid.UUID       `json:"trip_id"`
	Seq       int64           `json:"event_seq"`
	Kind      string          `json:"kind"`
	ActorID   *uuid.UUID      `json:"actor_id"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

type Invitation struct {
	ID        uuid.UUID  `json:"id"`
	TripID    uuid.UUID  `json:"trip_id"`
	ExpiresAt time.Time  `json:"expires_at"`
	MaxUses   int        `json:"max_uses"`
	UsedCount int        `json:"used_count"`
	RevokedAt *time.Time `json:"revoked_at"`
	CreatedAt time.Time  `json:"created_at"`
	// Code is only set in the response to creation; only its hash is stored.
	Code string `json:"code,omitempty"`
}

// palette gives members stable, distinguishable colours in join order.
var palette = []string{
	"#E53935", "#1E88E5", "#43A047", "#FB8C00", "#8E24AA", "#00ACC1",
	"#F4511E", "#3949AB", "#7CB342", "#D81B60", "#6D4C41", "#546E7A",
	"#FDD835", "#00897B", "#C0CA33",
}

func colorFor(index int) string { return palette[index%len(palette)] }
