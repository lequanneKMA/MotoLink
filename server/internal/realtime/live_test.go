package realtime

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func stores(t *testing.T) map[string]LiveStore {
	out := map[string]LiveStore{"mem": NewMemStore()}
	if addr := os.Getenv("TEST_REDIS_ADDR"); addr != "" {
		c := redis.NewClient(&redis.Options{Addr: addr})
		t.Cleanup(func() { c.Close() })
		out["redis"] = NewRedisStore(c)
	}
	return out
}

func pt(sid string, seq int64) LivePoint {
	return LivePoint{SessionID: sid, Seq: seq, Lat: 11.9, Lng: 108.4, ServerTs: time.Now().UnixMilli()}
}

const (
	s1 = "018f0000-0000-7000-8000-000000000001"
	s2 = "018f0000-0000-7000-8000-000000000002"
)

// docs/design/14 §3.1: the four ordering scenarios, on Redis (real Lua) and in memory.
func TestSetLocationOrdering(t *testing.T) {
	for name, st := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			trip, user := uuid.New(), uuid.New()
			check := func(p LivePoint, want bool) {
				t.Helper()
				got, err := st.SetLocation(ctx, trip, user, p)
				if err != nil || got != want {
					t.Fatalf("%s/%d => accepted=%v err=%v, want %v", p.SessionID[len(p.SessionID)-1:], p.Seq, got, err, want)
				}
			}
			check(pt(s1, 5), true)
			check(pt(s1, 3), false) // older seq
			check(pt(s1, 5), false) // duplicate
			check(pt(s1, 6), true)
			check(pt(s2, 1), true)    // newer session wins whatever the seq
			check(pt(s1, 100), false) // old session late
			got, _ := st.Locations(ctx, trip, []uuid.UUID{user, uuid.New()})
			if len(got) != 1 || got[user].SessionID != s2 || got[user].Seq != 1 {
				t.Fatalf("held: %+v", got)
			}
		})
	}
}

func TestSetLocationConcurrentKeepsMax(t *testing.T) {
	for name, st := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			trip, user := uuid.New(), uuid.New()
			var wg sync.WaitGroup
			for i := 1; i <= 100; i++ {
				wg.Add(1)
				go func(seq int64) { defer wg.Done(); _, _ = st.SetLocation(ctx, trip, user, pt(s1, seq)) }(int64(i))
			}
			wg.Wait()
			got, _ := st.Locations(ctx, trip, []uuid.UUID{user})
			if got[user].Seq != 100 {
				t.Fatalf("max seq must win, got %d", got[user].Seq)
			}
		})
	}
}

func TestOptionalFieldsRoundTrip(t *testing.T) {
	for name, st := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			trip, user := uuid.New(), uuid.New()
			acc, spd := float32(7.5), float32(9.25)
			p := pt(s1, 1)
			p.AccuracyM, p.SpeedMps = &acc, &spd
			_, _ = st.SetLocation(ctx, trip, user, p)
			got := mustLoc(t, st, trip, user)
			if got.AccuracyM == nil || *got.AccuracyM != 7.5 || *got.SpeedMps != 9.25 || got.Heading != nil {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func mustLoc(t *testing.T, st LiveStore, trip, user uuid.UUID) LivePoint {
	t.Helper()
	m, err := st.Locations(context.Background(), trip, []uuid.UUID{user})
	if err != nil || len(m) != 1 {
		t.Fatalf("locations: %v %v", m, err)
	}
	return m[user]
}

func TestPresenceLifecycle(t *testing.T) {
	for name, st := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			trip, user := uuid.New(), uuid.New()
			prev, err := st.ClaimConn(ctx, user, "A", "c1")
			if err != nil || prev != "" {
				t.Fatalf("first claim: %q %v", prev, err)
			}
			on, _ := st.PresenceTouch(ctx, trip, user)
			if !on {
				t.Fatal("first touch must report becameOnline")
			}
			if on, _ = st.PresenceTouch(ctx, trip, user); on {
				t.Fatal("second touch must not re-announce")
			}
			if m, _ := st.Online(ctx, trip, []uuid.UUID{user}); !m[user] {
				t.Fatal("must be online")
			}
			// a new connection (maybe on another instance) supersedes the old one
			prev, _ = st.ClaimConn(ctx, user, "B", "c2")
			if prev != "A|c1" {
				t.Fatalf("prev = %q", prev)
			}
			if ok, _ := st.TouchConn(ctx, user, "A", "c1"); ok {
				t.Fatal("old connection must have lost its claim")
			}
			// the old connection closing must NOT remove presence of the new one
			if rem, _ := st.ConnLeave(ctx, user, "A", "c1", []uuid.UUID{trip}); len(rem) != 0 {
				t.Fatalf("superseded leave removed presence: %v", rem)
			}
			if m, _ := st.Online(ctx, trip, []uuid.UUID{user}); !m[user] {
				t.Fatal("new connection must stay online")
			}
			rem, _ := st.ConnLeave(ctx, user, "B", "c2", []uuid.UUID{trip})
			if len(rem) != 1 || rem[0] != trip {
				t.Fatalf("leave must report the trip: %v", rem)
			}
			if m, _ := st.Online(ctx, trip, []uuid.UUID{user}); m[user] {
				t.Fatal("must be offline")
			}
		})
	}
}

func TestPresenceReapReportsEachUserOnce(t *testing.T) {
	m := NewMemStore().(*memStore)
	ctx := context.Background()
	trip, user := uuid.New(), uuid.New()
	now := time.Now()
	m.now = func() time.Time { return now }
	_, _ = m.PresenceTouch(ctx, trip, user)
	now = now.Add(60 * time.Second) // past the 45 s window
	if on, _ := m.Online(ctx, trip, []uuid.UUID{user}); on[user] {
		t.Fatal("expired presence must read offline")
	}
	first, _ := m.ReapPresence(ctx, trip)
	second, _ := m.ReapPresence(ctx, trip)
	if len(first) != 1 || len(second) != 0 {
		t.Fatalf("reap must report the user exactly once: %v then %v", first, second)
	}
}

func TestRedisKeysAlwaysHaveTTL(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR not set")
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	st := NewRedisStore(c)
	ctx := context.Background()
	trip, user := uuid.New(), uuid.New()
	_, _ = st.SetLocation(ctx, trip, user, pt(s1, 1))
	_, _ = st.ClaimConn(ctx, user, "A", "c")
	_, _ = st.PresenceTouch(ctx, trip, user)
	for _, k := range []string{kLoc(trip, user), kConn(user), kPres(trip)} {
		if ttl := c.TTL(ctx, k).Val(); ttl <= 0 {
			t.Errorf("%s must have a TTL (docs/design/08 §2), got %v", k, ttl)
		}
	}
}
