// Package pgtest creates an isolated PostgreSQL database per test.
//
// It needs TEST_DATABASE_URL pointing at any database on a server where the
// user may CREATE DATABASE (for example the compose postgres). Tests are skipped
// when it is unset, so `go test ./...` stays green without infrastructure; CI
// and `make test-int` set it. (docs/design/14 lists testcontainers as the target;
// a plain env URL is used first because it works with the compose stack and CI services.)
package pgtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"motolink/internal/platform/id"
	"motolink/internal/platform/pg"
)

// New returns the URL of a fresh, migrated database that is dropped at cleanup.
func New(t testing.TB) string {
	t.Helper()
	return create(t, true)
}

// NewEmpty returns a fresh database without migrations applied.
func NewEmpty(t testing.TB) string {
	t.Helper()
	return create(t, false)
}

func create(t testing.TB, migrate bool) string {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	defer admin.Close(ctx)

	name := "mltest_" + strings.ReplaceAll(id.NewString(), "-", "")
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q`, name)); err != nil {
		t.Fatalf("create database: %v", err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	dsn := u.String()

	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		a, err := pgx.Connect(c, base)
		if err != nil {
			return
		}
		defer a.Close(c)
		_, _ = a.Exec(c, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, name))
	})
	if migrate {
		if err := pg.Migrate(ctx, dsn, "up"); err != nil {
			t.Fatalf("migrate up: %v", err)
		}
	}
	return dsn
}

// Pool opens a pool on dsn and closes it at cleanup.
func Pool(t testing.TB, dsn string) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}
