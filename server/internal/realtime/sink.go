package realtime

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// DurablePoint is one validated GPS point headed for long-term storage (docs/design/09 §3.1).
type DurablePoint struct {
	TripID       uuid.UUID
	UserID       uuid.UUID
	SessionID    uuid.UUID
	SessionMonth time.Time // first day of the month the session started (partition key)
	Seq          int64
	RecordedAt   time.Time
	ReceivedAt   time.Time
	Lat, Lng     float64
	AccuracyM    *float32
	SpeedMps     *float32
	HeadingDeg   *float32
	AltitudeM    *float64
	Suspect      bool
}

// ErrSinkFull means the durable branch cannot accept more right now. The point is NOT acknowledged,
// so the client keeps it and resends (ADR-005 mode 3).
var ErrSinkFull = errors.New("realtime: durable sink full")

// Sink is the durable branch (Kafka or direct PostgreSQL, ADR-005/007). Enqueue must never block.
// The sink reports each point through the done callback once it is durable (acks=all / COMMIT);
// only then is it covered by location.ack.
type Sink interface {
	Enqueue(p DurablePoint) error
	SetDone(fn func(sessionID uuid.UUID, seq int64))
}

// SOSHandler processes SOS messages arriving over the WebSocket (implemented by the sos module).
type SOSHandler interface {
	Create(ctx Ctx, userID, tripID uuid.UUID, msgID string, payload []byte) (map[string]any, error)
	Receipt(ctx Ctx, userID, tripID uuid.UUID, payload []byte) error
}

// Authenticator validates the bearer token of an upgrade request / auth.refresh.
type Authenticator interface {
	Authenticate(ctx Ctx, token string) (Identity, error)
}

// Identity is a verified caller.
type Identity struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	ExpiresAt time.Time
}

// ErrTokenExpired / ErrUnauthorized classify Authenticator failures.
var (
	ErrTokenExpired = errors.New("realtime: token expired")
	ErrUnauthorized = errors.New("realtime: unauthorized")
)
