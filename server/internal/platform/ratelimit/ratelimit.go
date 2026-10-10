// Package ratelimit implements fixed-window counters (docs/design/08 §4.3).
package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter counts hits per key within a window.
type Limiter interface {
	// Hit records one event and returns the count in the current window and
	// the seconds until the window resets.
	Hit(ctx context.Context, key string, window time.Duration) (count int64, retryAfter int, err error)
}

var incr = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
return {n, redis.call('TTL', KEYS[1])}
`)

// Redis is a Limiter backed by Redis keys ml:rl:<key>.
type Redis struct{ C redis.Scripter }

func (r Redis) Hit(ctx context.Context, key string, window time.Duration) (int64, int, error) {
	res, err := incr.Run(ctx, r.C, []string{"ml:rl:" + key}, int(window.Seconds())).Int64Slice()
	if err != nil {
		return 0, 0, err
	}
	ttl := int(res[1])
	if ttl < 0 {
		ttl = int(window.Seconds())
	}
	return res[0], ttl, nil
}

// Memory is a process-local Limiter (tests, and fallback when Redis is not configured).
type Memory struct {
	mu  sync.Mutex
	now func() time.Time
	m   map[string]*entry
}
type entry struct {
	n   int64
	exp time.Time
}

func NewMemory() *Memory { return &Memory{now: time.Now, m: map[string]*entry{}} }

func (l *Memory) Hit(_ context.Context, key string, window time.Duration) (int64, int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	e := l.m[key]
	if e == nil || !now.Before(e.exp) {
		e = &entry{exp: now.Add(window)}
		l.m[key] = e
	}
	e.n++
	return e.n, int(e.exp.Sub(now).Seconds()) + 1, nil
}
