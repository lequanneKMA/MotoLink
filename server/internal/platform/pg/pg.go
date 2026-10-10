// Package pg wires the PostgreSQL pool and migrations.
package pg

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"motolink/db"
)

// NewPool opens a pgx pool. maxConns bounds backend connections (docs/design/13 §2: max_connections=50).
func NewPool(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.Ping(pctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return p, nil
}

// Migrate runs goose with the embedded migrations. cmd: up | down | status | reset.
func Migrate(ctx context.Context, url, cmd string) error {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	sqlDB := stdlib.OpenDB(*cfg.ConnConfig)
	defer sqlDB.Close()
	return migrateDB(ctx, sqlDB, cmd)
}

func migrateDB(ctx context.Context, sqlDB *sql.DB, cmd string) error {
	sub, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		return err
	}
	goose.SetBaseFS(nil)
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, sub)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	switch cmd {
	case "up":
		_, err = provider.Up(ctx)
	case "down":
		_, err = provider.Down(ctx)
	case "reset":
		_, err = provider.DownTo(ctx, 0)
	case "status":
		var st []*goose.MigrationStatus
		if st, err = provider.Status(ctx); err == nil {
			for _, s := range st {
				fmt.Printf("%-8s %s\n", s.State, s.Source.Path)
			}
		}
	default:
		return fmt.Errorf("unknown migrate command %q (up|down|reset|status)", cmd)
	}
	return err
}
