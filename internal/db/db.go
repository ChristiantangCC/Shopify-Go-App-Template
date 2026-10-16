// Package db owns the Postgres pool and schema migrations. Migrations run at
// boot: goose applies the SQL files embedded below, then River installs its
// job tables, so a fresh database is ready after the first start.
package db

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Connect opens a pgx pool and verifies the database answers.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: %w", err)
	}
	return pool, nil
}

// Migrate brings the schema up to date: the app's goose migrations first,
// then River's queue tables. An advisory lock serialises it, so several
// instances booting at once (a rolling deploy) migrate one at a time.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	lock, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer lock.Release()
	if _, err := lock.Exec(ctx, "SELECT pg_advisory_lock(hashtext('schema-migrations'))"); err != nil {
		return fmt.Errorf("db: migration lock: %w", err)
	}
	defer lock.Exec(ctx, "SELECT pg_advisory_unlock(hashtext('schema-migrations'))")

	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("db: %w", err)
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}

	riverMigrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("db: river migrator: %w", err)
	}
	if _, err := riverMigrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("db: river migrate: %w", err)
	}
	return nil
}
