package identity

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Revoker marks auth sessions (refresh families) revoked so access tokens die
// before expiry (docs/design/08: ml:revoked:sid:<sid>, TTL = access token TTL).
type Revoker interface {
	Revoke(ctx context.Context, sid uuid.UUID, ttl time.Duration) error
	IsRevoked(ctx context.Context, sid uuid.UUID) (bool, error)
}

type redisRevoker struct{ c redis.Cmdable }

// NewRedisRevoker returns a Revoker backed by Redis.
func NewRedisRevoker(c redis.Cmdable) Revoker { return redisRevoker{c: c} }

func key(sid uuid.UUID) string { return "ml:revoked:sid:" + sid.String() }

func (r redisRevoker) Revoke(ctx context.Context, sid uuid.UUID, ttl time.Duration) error {
	return r.c.Set(ctx, key(sid), "1", ttl).Err()
}

func (r redisRevoker) IsRevoked(ctx context.Context, sid uuid.UUID) (bool, error) {
	n, err := r.c.Exists(ctx, key(sid)).Result()
	return n > 0, err
}

// ErrNoRevoker makes Service.SessionActive fall back to PostgreSQL.
var ErrNoRevoker = errors.New("identity: no revocation cache configured")

// NoopRevoker is used when Redis is not configured: every check falls back to PostgreSQL.
type NoopRevoker struct{}

func (NoopRevoker) Revoke(context.Context, uuid.UUID, time.Duration) error { return nil }
func (NoopRevoker) IsRevoked(context.Context, uuid.UUID) (bool, error)     { return false, ErrNoRevoker }
