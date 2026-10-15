// Package testdb gives integration tests a private, migrated Postgres
// database. Tests skip when TEST_DATABASE_URL is unset, so `go test ./...`
// works without Postgres and CI (which has it) covers the full suite.
package testdb

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/db"
)

// Pool creates a throwaway database, migrates it and returns a pool bound
// to it. The database is dropped when the test finishes.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping Postgres integration test")
	}
	ctx := context.Background()

	admin, err := db.Connect(ctx, baseURL)
	if err != nil {
		t.Fatalf("testdb: connect: %v", err)
	}

	name := fmt.Sprintf("test_%d_%d", os.Getpid(), rand.Int63())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close()
		t.Fatalf("testdb: create database: %v", err)
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("testdb: %v", err)
	}
	parsed.Path = "/" + name

	pool, err := db.Connect(ctx, parsed.String())
	if err != nil {
		t.Fatalf("testdb: connect to %s: %v", name, err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+name); err != nil {
			t.Errorf("testdb: drop database: %v", err)
		}
		admin.Close()
	})

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}
	return pool
}
