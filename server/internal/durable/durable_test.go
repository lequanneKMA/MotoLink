package durable

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"motolink/internal/platform/id"
	"motolink/internal/realtime"
	"motolink/internal/testutil/pgtest"
	"motolink/internal/trip"
)

func seed(t *testing.T, db *pgxpool.Pool) (user, tripID, sess uuid.UUID, month time.Time) {
	t.Helper()
	ctx := context.Background()
	user, tripID, sess = id.New(), id.New(), id.New()
	month = time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.Exec(ctx, `INSERT INTO users (id,email,password_hash,display_name) VALUES ($1,$2,'x','U')`, user, user.String()+"@t.io"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO trips (id,owner_id,name) VALUES ($1,$2,'T')`, tripID, user); err != nil {
		t.Fatal(err)
	}
	return
}

func TestPGSinkBatchesIdempotentlyAndAcks(t *testing.T) {
	db := pgtest.Pool(t, pgtest.New(t))
	user, tripID, sess, month := seed(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	s := NewPGSink(db, PGSinkOptions{BatchSize: 10, FlushEvery: 20 * time.Millisecond})
	var mu sync.Mutex
	acked := map[int64]int{}
	s.SetDone(func(_ uuid.UUID, seq int64) { mu.Lock(); acked[seq]++; mu.Unlock() })
	s.Start(ctx)
	now := time.Now()
	for i := int64(1); i <= 25; i++ {
		if err := s.Enqueue(realtime.DurablePoint{TripID: tripID, UserID: user, SessionID: sess, SessionMonth: month, Seq: i,
			RecordedAt: now, ReceivedAt: now, Lat: 10, Lng: 106}); err != nil {
			t.Fatal(err)
		}
	}
	// duplicates of seq 1..3 (client resend) must not fail or multiply rows
	for i := int64(1); i <= 3; i++ {
		_ = s.Enqueue(realtime.DurablePoint{TripID: tripID, UserID: user, SessionID: sess, SessionMonth: month, Seq: i,
			RecordedAt: now, ReceivedAt: now, Lat: 10, Lng: 106})
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(acked)
		mu.Unlock()
		if n == 25 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	s.Wait()
	var cnt int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM location_points WHERE session_id=$1`, sess).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 25 {
		t.Fatalf("rows=%d want 25", cnt)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(acked) != 25 {
		t.Fatalf("acked %d seqs want 25", len(acked))
	}
}

func TestPGSinkFullQueue(t *testing.T) {
	db := pgtest.Pool(t, pgtest.New(t))
	s := NewPGSink(db, PGSinkOptions{QueueSize: 2}) // not started: nothing drains
	for i := 0; i < 2; i++ {
		if err := s.Enqueue(realtime.DurablePoint{Seq: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Enqueue(realtime.DurablePoint{}); err != realtime.ErrSinkFull {
		t.Fatalf("got %v want ErrSinkFull", err)
	}
}

type recFanout struct {
	mu  sync.Mutex
	evs []trip.Event
}

func (r *recFanout) PublishEvent(_ context.Context, ev trip.Event) {
	r.mu.Lock()
	r.evs = append(r.evs, ev)
	r.mu.Unlock()
}

func TestRelayPublishesOnceInOrderAndRetriesOnFailure(t *testing.T) {
	db := pgtest.Pool(t, pgtest.New(t))
	ctx := context.Background()
	user, tripID, _, _ := seed(t, db)
	for i := 0; i < 5; i++ {
		tx, _ := db.Begin(ctx)
		if _, err := trip.AppendEvent(ctx, tx, tripID, &user, trip.KindMemberJoined, map[string]any{"i": i}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	rec := &recFanout{}
	d := &Dispatcher{Fanout: rec}
	fail := true
	pub := PublisherFunc(func(c context.Context, m []Message) error {
		if fail {
			return context.DeadlineExceeded
		}
		return d.Publisher().Publish(c, m)
	})
	r := NewRelay(db, pub, RelayOptions{BatchSize: 3})
	if _, err := r.Once(ctx); err == nil {
		t.Fatal("expected publish error")
	}
	var unpub int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE published_at IS NULL`).Scan(&unpub)
	if unpub != 5 {
		t.Fatalf("unpublished=%d want 5 (failure must not mark rows)", unpub)
	}
	fail = false
	for {
		n, err := r.Once(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	if len(rec.evs) != 5 {
		t.Fatalf("delivered %d want 5", len(rec.evs))
	}
	for i, ev := range rec.evs {
		if ev.Seq != int64(i+1) {
			t.Fatalf("event %d has seq %d", i, ev.Seq)
		}
		var d map[string]int
		_ = json.Unmarshal(ev.Data, &d)
		if d["i"] != i {
			t.Fatalf("payload order broken: %v", d)
		}
	}
}

func TestDispatcherDropsMalformed(t *testing.T) {
	rec := &recFanout{}
	d := &Dispatcher{Fanout: rec}
	if err := d.Handle(context.Background(), []byte(`{"nope":1}`)); err != nil {
		t.Fatal(err)
	}
	if len(rec.evs) != 0 {
		t.Fatal("malformed payload delivered")
	}
}

func TestBisectIsolatesPoisonRecordAgainstRealPG(t *testing.T) {
	db := pgtest.Pool(t, pgtest.New(t))
	user, tripID, sess, month := seed(t, db)
	now := time.Now()
	var pts []realtime.DurablePoint
	for i := int64(1); i <= 9; i++ {
		lat := 10.0
		if i == 6 {
			lat = 999 // violates CHECK (lat BETWEEN -90 AND 90)
		}
		pts = append(pts, realtime.DurablePoint{TripID: tripID, UserID: user, SessionID: sess, SessionMonth: month, Seq: i,
			RecordedAt: now, ReceivedAt: now, Lat: lat, Lng: 106})
	}
	var bad []int64
	err := writeWithBisect(context.Background(), pts,
		func(c context.Context, b []realtime.DurablePoint) error { return InsertPoints(c, db, b) },
		func(p realtime.DurablePoint, _ error) { bad = append(bad, p.Seq) })
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 1 || bad[0] != 6 {
		t.Fatalf("bad=%v want [6]", bad)
	}
	var n int
	_ = db.QueryRow(context.Background(), `SELECT count(*) FROM location_points WHERE session_id=$1`, sess).Scan(&n)
	if n != 8 {
		t.Fatalf("rows=%d want 8", n)
	}
}

func TestBisectReturnsInfrastructureErrorsForRetry(t *testing.T) {
	boom := context.DeadlineExceeded
	err := writeWithBisect(context.Background(), make([]realtime.DurablePoint, 4),
		func(context.Context, []realtime.DurablePoint) error { return boom },
		func(realtime.DurablePoint, error) { t.Fatal("must not dead-letter on infra error") })
	if err != boom {
		t.Fatalf("got %v", err)
	}
}

func TestPointCodecRoundTrip(t *testing.T) {
	acc := float32(7.5)
	in := realtime.DurablePoint{TripID: id.New(), UserID: id.New(), SessionID: id.New(),
		SessionMonth: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Seq: 813, RecordedAt: time.UnixMilli(1760072402890),
		ReceivedAt: time.UnixMilli(1760072403100), Lat: 11.94052, Lng: 108.45841, AccuracyM: &acc, Suspect: true}
	b, err := encodePoint(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := decodePoint(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.Seq != in.Seq || out.Lat != in.Lat || !out.SessionMonth.Equal(in.SessionMonth) || !out.Suspect || *out.AccuracyM != acc ||
		!out.RecordedAt.Equal(in.RecordedAt) {
		t.Fatalf("%+v != %+v", out, in)
	}
	if _, err := decodePoint([]byte(`{"seq":0}`)); err == nil {
		t.Fatal("invalid record accepted")
	}
}
