// Package db owns the Postgres connection and schema migrations. One pgx
// pool backs everything: River's workers use it directly, and GORM runs on a
// database/sql handle opened from the same pool, so the app never holds two
// sets of connections. Migrations run at boot: GORM's AutoMigrate creates or
// extends the app's tables from the Go models, then River installs its job
// tables, so a fresh database is ready after the first start.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// slowQuery is the duration above which GORM logs a query as slow.
const slowQuery = 200 * time.Millisecond

// DB bundles the three handles onto the one pool. Close releases them all.
type DB struct {
	// Pool is the pgx pool: River's workers and the health check use it.
	Pool *pgxpool.Pool
	// SQL is a database/sql view of Pool. River's insert-only client uses
	// it to enqueue jobs inside a GORM transaction.
	SQL *sql.DB
	// Gorm is the ORM every app query goes through.
	Gorm *gorm.DB
}

// Close shuts the database/sql handle, then the pool under it.
func (d *DB) Close() {
	d.SQL.Close()
	d.Pool.Close()
}

// Connect opens a pgx pool, verifies the database answers and layers
// database/sql and GORM on top of it.
func Connect(ctx context.Context, databaseURL string, logger *slog.Logger) (*DB, error) {
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

	sqlDB := stdlib.OpenDBFromPool(pool)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger: gormlogger.NewSlogLogger(logger, gormlogger.Config{
			SlowThreshold:             slowQuery,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
			LogLevel:                  gormlogger.Warn,
		}),
		// Every write is a single statement or an explicit transaction;
		// GORM's implicit per-write transaction is only extra round trips.
		SkipDefaultTransaction: true,
		TranslateError:         true,
	})
	if err != nil {
		sqlDB.Close()
		pool.Close()
		return nil, fmt.Errorf("db: gorm: %w", err)
	}
	return &DB{Pool: pool, SQL: sqlDB, Gorm: gormDB}, nil
}

// Migrate brings the schema up to date: GORM's AutoMigrate for the given
// models first, then River's queue tables. An advisory lock serialises it,
// so several instances booting at once (a rolling deploy) migrate one at a
// time.
//
// AutoMigrate only adds: new tables, columns and indexes. Dropping or
// renaming a column is a deliberate manual step (see the README).
func Migrate(ctx context.Context, d *DB, models ...any) error {
	lock, err := d.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer lock.Release()
	if _, err := lock.Exec(ctx, "SELECT pg_advisory_lock(hashtext('schema-migrations'))"); err != nil {
		return fmt.Errorf("db: migration lock: %w", err)
	}
	defer lock.Exec(ctx, "SELECT pg_advisory_unlock(hashtext('schema-migrations'))")

	if err := d.Gorm.WithContext(ctx).AutoMigrate(models...); err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}

	riverMigrator, err := rivermigrate.New(riverpgxv5.New(d.Pool), nil)
	if err != nil {
		return fmt.Errorf("db: river migrator: %w", err)
	}
	if _, err := riverMigrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("db: river migrate: %w", err)
	}
	return nil
}
