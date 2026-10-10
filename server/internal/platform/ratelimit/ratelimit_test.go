package ratelimit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"motolink/internal/platform/id"
)

func TestMemoryWindow(t *testing.T) {
	l := NewMemory()
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := int64(1); i <= 3; i++ {
		if n, _, _ := l.Hit(context.Background(), "k", time.Minute); n != i {
			t.Fatalf("hit %d => %d", i, n)
		}
	}
	now = now.Add(61 * time.Second)
	if n, _, _ := l.Hit(context.Background(), "k", time.Minute); n != 1 {
		t.Fatalf("window must reset, got %d", n)
	}
}

func TestRedisFixedWindow(t *testing.T) { // real Redis: set TEST_REDIS_ADDR
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR not set")
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	l := Redis{C: c}
	key := "test:" + id.NewString()
	for i := int64(1); i <= 3; i++ {
		n, ttl, err := l.Hit(context.Background(), key, 30*time.Second)
		if err != nil || n != i || ttl <= 0 || ttl > 30 {
			t.Fatalf("hit %d: n=%d ttl=%d err=%v", i, n, ttl, err)
		}
	}
	if ttl := c.TTL(context.Background(), "ml:rl:"+key).Val(); ttl <= 0 {
		t.Fatalf("every rate-limit key must have a TTL (docs/design/08 §2), got %v", ttl)
	}
}
