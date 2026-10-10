package trip

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SessionEnd describes a tracking session closed by a trip/membership change (event payload).
type SessionEnd struct {
	SessionID uuid.UUID `json:"session_id"`
	UserID    uuid.UUID `json:"user_id"`
	EndReason string    `json:"end_reason"`
}

// EndOpenSessionsTx closes open tracking sessions of a trip (optionally of one user).
// Lives here because trip lifecycle and membership changes must do it atomically.
func EndOpenSessionsTx(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, userID *uuid.UUID, reason string) ([]SessionEnd, error) {
	rows, err := tx.Query(ctx, `
		UPDATE tracking_sessions SET status='ended', end_reason=$3, ended_at=now()
		WHERE trip_id=$1 AND ($2::uuid IS NULL OR user_id=$2) AND status IN ('active','paused')
		RETURNING id, user_id`, tripID, userID, reason)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SessionEnd, error) {
		s := SessionEnd{EndReason: reason}
		err := r.Scan(&s.SessionID, &s.UserID)
		return s, err
	})
}
