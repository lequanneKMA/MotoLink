package jobs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"motolink/internal/platform/id"
	"motolink/internal/testutil/pgtest"
	"motolink/internal/tracking"
	"motolink/internal/trip"
)

func TestLocalLockerIsExclusive(t *testing.T) {
	l := &LocalLocker{}
	u := l.TryLock(context.Background(), "j", time.Second)
	if u == nil || l.TryLock(context.Background(), "j", time.Second) != nil {
		t.Fatal("second holder must be refused")
	}
	u()
	if l.TryLock(context.Background(), "j", time.Second) == nil {
		t.Fatal("lock must be free after unlock")
	}
}

func TestRunOnceSkipsWhenLockedAndCountsItems(t *testing.T) {
	l := &LocalLocker{}
	var runs atomic.Int32
	s := NewScheduler(nil, l, nil, nil)
	j := Job{Name: "x", Every: time.Second, Run: func(context.Context) (int, error) { runs.Add(1); return 2, nil }}
	hold := l.TryLock(context.Background(), "x", time.Second)
	s.RunOnce(context.Background(), j)
	if runs.Load() != 0 {
		t.Fatal("ran while locked")
	}
	hold()
	s.RunOnce(context.Background(), j)
	if runs.Load() != 1 {
		t.Fatal("did not run")
	}
}

func TestStandardCleanupJobsAgainstRealPG(t *testing.T) {
	db := pgtest.Pool(t, pgtest.New(t))
	ctx := context.Background()
	trips := trip.NewService(db, nil)
	sessions := tracking.NewService(db, trips, nil, nil)
	user := id.New()
	if _, err := db.Exec(ctx, `INSERT INTO users (id,email,password_hash,display_name) VALUES ($1,$2,'x','U')`, user, user.String()+"@t.io"); err != nil {
		t.Fatal(err)
	}
	tr := id.New()
	_, _ = db.Exec(ctx, `INSERT INTO trips (id,owner_id,name) VALUES ($1,$2,'T')`, tr, user)
	_, err := db.Exec(ctx, `INSERT INTO outbox (topic,msg_key,event_id,payload,created_at,published_at) VALUES
		('t','k',$1,'{}', now()-interval '9 days', now()-interval '8 days'),
		('t','k',$2,'{}', now(), now()),
		('t','k',$3,'{}', now(), NULL)`, id.New(), id.New(), id.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range Standard(db, sessions, trips, Config{}) {
		if _, err := j.Run(ctx); err != nil { // every standard job must execute without error on a fresh schema
			t.Fatalf("job %s: %v", j.Name, err)
		}
	}
	var left int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&left)
	if left != 2 {
		t.Fatalf("outbox rows after cleanup=%d want 2 (old published row removed, unpublished kept)", left)
	}
}
