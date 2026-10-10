package pg_test

import (
	"context"
	"testing"
	"time"

	"motolink/internal/platform/id"
	"motolink/internal/platform/pg"
	"motolink/internal/testutil/pgtest"
)

var wantTables = []string{
	"users", "devices", "refresh_tokens", "trips", "trip_members", "trip_invitations",
	"trip_events", "tracking_sessions", "location_points", "sos_events", "sos_recipients",
	"notifications", "consent_records", "idempotency_keys", "outbox",
}

func TestMigrateUpCreatesSchemaFrom05(t *testing.T) {
	dsn := pgtest.New(t)
	p := pgtest.Pool(t, dsn)
	ctx := context.Background()
	for _, tb := range wantTables {
		var n int
		if err := p.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name=$1`, tb).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s missing (n=%d err=%v)", tb, n, err)
		}
	}
	// current month partition must exist (05 §3)
	var parts int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM pg_inherits i JOIN pg_class c ON c.oid=i.inhparent WHERE c.relname='location_points'`).Scan(&parts); err != nil || parts < 4 {
		t.Fatalf("expected >=4 partitions, got %d err=%v", parts, err)
	}
}

func TestMigrateDownAndUpAgain(t *testing.T) {
	dsn := pgtest.New(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, dsn, "reset"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	p := pgtest.Pool(t, dsn)
	var n int
	_ = p.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='users'`).Scan(&n)
	if n != 0 {
		t.Fatal("users table should be gone after reset")
	}
	if err := pg.Migrate(ctx, dsn, "up"); err != nil {
		t.Fatalf("up again: %v", err)
	}
}

func TestLocationPointsIdempotentInsert(t *testing.T) {
	dsn := pgtest.New(t)
	p := pgtest.Pool(t, dsn)
	ctx := context.Background()
	month := time.Now().UTC().Format("2006-01") + "-01"
	sid, trip, user := id.New(), id.New(), id.New()
	const q = `INSERT INTO location_points (session_month, session_id, seq, trip_id, user_id, recorded_at, received_at, lat, lng)
	           VALUES ($1,$2,$3,$4,$5, now(), now(), 11.9, 108.4) ON CONFLICT (session_month, session_id, seq) DO NOTHING`
	for i := 0; i < 3; i++ { // same (session, seq) three times: ADR-005 retry safety
		if _, err := p.Exec(ctx, q, month, sid, 1, trip, user); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	_ = p.QueryRow(ctx, `SELECT count(*) FROM location_points WHERE session_id=$1`, sid).Scan(&n)
	if n != 1 {
		t.Fatalf("want 1 row after 3 identical inserts, got %d", n)
	}
	if _, err := p.Exec(ctx, `INSERT INTO location_points (session_month, session_id, seq, trip_id, user_id, recorded_at, received_at, lat, lng)
	   VALUES ($1,$2,2,$3,$4, now(), now(), 91, 0)`, month, sid, trip, user); err == nil {
		t.Fatal("lat=91 must violate CHECK")
	}
}

func TestConstraintsOneOwnerAndOneOpenSession(t *testing.T) {
	dsn := pgtest.New(t)
	p := pgtest.Pool(t, dsn)
	ctx := context.Background()
	u1, u2, trip := id.New(), id.New(), id.New()
	for _, u := range []any{u1, u2} {
		if _, err := p.Exec(ctx, `INSERT INTO users (id,email,display_name) VALUES ($1, $2, 'x')`, u, id.NewString()+"@x.vn"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Exec(ctx, `INSERT INTO trips (id, owner_id, name) VALUES ($1,$2,'t')`, trip, u1); err != nil {
		t.Fatal(err)
	}
	ins := `INSERT INTO trip_members (trip_id,user_id,role,status) VALUES ($1,$2,$3,'active')`
	if _, err := p.Exec(ctx, ins, trip, u1, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, ins, trip, u2, "owner"); err == nil {
		t.Fatal("second active owner must be rejected")
	}
	mk := `INSERT INTO tracking_sessions (id,trip_id,user_id,session_month) VALUES ($1,$2,$3,date_trunc('month',now())::date)`
	if _, err := p.Exec(ctx, mk, id.New(), trip, u1); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, mk, id.New(), trip, u1); err == nil {
		t.Fatal("second open session for same (trip,user) must be rejected")
	}
}
