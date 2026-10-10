// Package realtime implements the WebSocket layer: connections, hub, ingest, presence and
// cross-instance fan-out (docs/design/07, 08, 10; ADR-005, 006, 009, 011).
package realtime

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	ProtoVersion = 1

	// close codes (docs/design/07 §4)
	CloseGoingAway   = 1001
	CloseBadMessage  = 1008
	CloseTooBig      = 1009
	CloseTryLater    = 1013
	CloseAuthExpired = 4401
	CloseForbidden   = 4403
	CloseSuperseded  = 4409
	CloseRateLimited = 4429
)

// Envelope is the frame of every message in both directions.
type Envelope struct {
	V        int             `json:"v"`
	Type     string          `json:"type"`
	ID       string          `json:"id,omitempty"`
	TripID   string          `json:"trip_id,omitempty"`
	Ts       int64           `json:"ts,omitempty"`
	EventSeq int64           `json:"event_seq,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// ---- client -> server payloads ----

type ResumeEntry struct {
	TripID       string `json:"trip_id"`
	LastEventSeq int64  `json:"last_event_seq"`
}

type HelloPayload struct {
	DeviceID     string        `json:"device_id"`
	AppVersion   string        `json:"app_version"`
	OS           string        `json:"os"`
	ClientTimeMs int64         `json:"client_time_ms"`
	SupportedV   []int         `json:"supported_v"`
	Resume       []ResumeEntry `json:"resume"`
}

type WirePoint struct {
	Seq       int64    `json:"seq"`
	ClientTs  int64    `json:"client_ts"`
	Lat       float64  `json:"lat"`
	Lng       float64  `json:"lng"`
	AccuracyM *float32 `json:"accuracy_m,omitempty"`
	SpeedMps  *float32 `json:"speed_mps,omitempty"`
	Heading   *float32 `json:"heading_deg,omitempty"`
	AltitudeM *float64 `json:"altitude_m,omitempty"`
}

type LocationUpdatePayload struct {
	SessionID string      `json:"session_id"`
	Points    []WirePoint `json:"points"`
}

type TripJoinPayload struct {
	LastEventSeq int64 `json:"last_event_seq"`
}

type AuthRefreshPayload struct {
	AccessToken string `json:"access_token"`
}

type EventAckPayload struct {
	EventSeq int64 `json:"event_seq"`
}

// ---- server -> client payloads ----

type WelcomePayload struct {
	ConnID       string    `json:"conn_id"`
	UserID       string    `json:"user_id"`
	ServerTimeMs int64     `json:"server_time_ms"`
	ClockOffset  int64     `json:"clock_offset_ms"`
	VMin         int       `json:"v_min"`
	VMax         int       `json:"v_max"`
	Heartbeat    Heartbeat `json:"heartbeat"`
	Limits       Limits    `json:"limits"`
}

type Heartbeat struct {
	PingIntervalS int `json:"server_ping_interval_s"`
	PongTimeoutS  int `json:"pong_timeout_s"`
}

type Limits struct {
	MaxFrameBytes     int `json:"max_frame_bytes"`
	MaxPointsPerFrame int `json:"max_points_per_frame"`
}

type TripBrief struct {
	Status string `json:"status"`
	Name   string `json:"name"`
}

type ReplayInfo struct {
	FromSeq  int64 `json:"from_seq"`
	Count    int   `json:"count"`
	Complete bool  `json:"complete"`
}

type SnapshotMember struct {
	UserID       string     `json:"user_id"`
	DisplayName  string     `json:"display_name"`
	Color        string     `json:"color"`
	Role         string     `json:"role"`
	SharingState string     `json:"sharing_state"`
	Presence     string     `json:"presence"`
	Location     *LivePoint `json:"location"`
}

type TripJoinedPayload struct {
	Trip         TripBrief        `json:"trip"`
	HeadEventSeq int64            `json:"head_event_seq"`
	Replay       ReplayInfo       `json:"replay"`
	Members      []SnapshotMember `json:"members"`
	Degraded     bool             `json:"degraded,omitempty"`
}

// LivePoint is a member's position as sent to viewers. UserID is empty inside snapshots (the member carries it).
type LivePoint struct {
	TripID    string   `json:"-"` // routing only: positions are grouped per trip when flushed
	UserID    string   `json:"user_id,omitempty"`
	SessionID string   `json:"session_id"`
	Seq       int64    `json:"seq"`
	Lat       float64  `json:"lat"`
	Lng       float64  `json:"lng"`
	AccuracyM *float32 `json:"accuracy_m,omitempty"`
	SpeedMps  *float32 `json:"speed_mps,omitempty"`
	Heading   *float32 `json:"heading_deg,omitempty"`
	ServerTs  int64    `json:"server_ts"`
}

// newer reports whether a is strictly newer than b by (session_id, seq): ADR-004.
func (a LivePoint) newer(b LivePoint) bool {
	if a.SessionID != b.SessionID {
		return a.SessionID > b.SessionID
	}
	return a.Seq > b.Seq
}

type LocationEventPayload struct {
	Points []LivePoint `json:"points"`
}

type RejectedPoint struct {
	Seq    int64  `json:"seq"`
	Reason string `json:"reason"`
}

type LocationAckPayload struct {
	SessionID string          `json:"session_id"`
	UpToSeq   int64           `json:"up_to_seq"`
	Rejected  []RejectedPoint `json:"rejected,omitempty"`
}

type PresencePayload struct {
	UserID string `json:"user_id"`
	State  string `json:"state"`
	At     int64  `json:"at"`
}

type TripEventPayload struct {
	EventID string          `json:"event_id"`
	Kind    string          `json:"kind"`
	ActorID *string         `json:"actor_id,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type AckPayload struct {
	Ref    string         `json:"ref"`
	OK     bool           `json:"ok"`
	Result map[string]any `json:"result,omitempty"`
}

type ErrorPayload struct {
	Ref        string `json:"ref,omitempty"`
	Code       string `json:"code"`
	Message    string `json:"message,omitempty"`
	Retryable  bool   `json:"retryable"`
	RetryAfter int64  `json:"retry_after_ms,omitempty"`
}

type ResyncPayload struct {
	Reason string `json:"reason"`
}

type ClosingPayload struct {
	Reason       string `json:"reason"`
	RetryAfterMs int64  `json:"retry_after_ms,omitempty"`
}

// ---- helpers ----

func marshalEnv(typ, id, tripID string, eventSeq int64, payload any) []byte {
	var raw json.RawMessage
	if payload != nil {
		raw, _ = json.Marshal(payload)
	}
	b, _ := json.Marshal(Envelope{V: ProtoVersion, Type: typ, ID: id, TripID: tripID, Ts: time.Now().UnixMilli(), EventSeq: eventSeq, Payload: raw})
	return b
}

func errFrame(ref, code, msg string, retryable bool, retryAfterMs int64) []byte {
	return marshalEnv("error", "", "", 0, ErrorPayload{Ref: ref, Code: code, Message: msg, Retryable: retryable, RetryAfter: retryAfterMs})
}

func ackFrame(ref string, result map[string]any) []byte {
	return marshalEnv("ack", "", "", 0, AckPayload{Ref: ref, OK: true, Result: result})
}

func tripEventFrame(tripID uuid.UUID, seq int64, ep TripEventPayload, typ string) []byte {
	return marshalEnv(typ, "", tripID.String(), seq, ep)
}
