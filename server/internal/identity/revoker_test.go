package identity

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestRedisRevokerHasTTL(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR not set")
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	r := NewRedisRevoker(c)
	ctx := context.Background()
	sid := uuid.New()
	if rev, err := r.IsRevoked(ctx, sid); err != nil || rev {
		t.Fatalf("fresh sid: rev=%v err=%v", rev, err)
	}
	if err := r.Revoke(ctx, sid, 15*time.Minute); err != nil {
		t.Fatal(err)
	}
	if rev, _ := r.IsRevoked(ctx, sid); !rev {
		t.Fatal("must be revoked")
	}
	if ttl := c.TTL(ctx, "ml:revoked:sid:"+sid.String()).Val(); ttl <= 0 || ttl > 15*time.Minute {
		t.Fatalf("revocation marker needs a TTL <= access TTL, got %v", ttl)
	}
}

func TestNoopRevokerForcesPostgresFallback(t *testing.T) {
	if _, err := (NoopRevoker{}).IsRevoked(context.Background(), uuid.New()); err != ErrNoRevoker {
		t.Fatalf("got %v", err)
	}
}
