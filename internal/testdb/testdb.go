// Package testdb gives integration tests a private, migrated Postgres
// database. Tests skip when TEST_DATABASE_URL is unset, so `go test ./...`
// works without Postgres and CI (which has it) covers the full suite.
package testdb

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/url"
	"os"
	"testing"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/db"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
)

// Open creates a throwaway database, migrates it and returns handles bound
// to it. The database is dropped when the test finishes.
func Open(t *testing.T) *db.DB {
	t.Helper()
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping Postgres integration test")
	}
	ctx := context.Background()
	logger := slog.New(slog.DiscardHandler)

	admin, err := db.Connect(ctx, baseURL, logger)
	if err != nil {
		t.Fatalf("testdb: connect: %v", err)
	}

	name := fmt.Sprintf("test_%d_%d", os.Getpid(), rand.Int63())
	if _, err := admin.Pool.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close()
		t.Fatalf("testdb: create database: %v", err)
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("testdb: %v", err)
	}
	parsed.Path = "/" + name

	database, err := db.Connect(ctx, parsed.String(), logger)
	if err != nil {
		t.Fatalf("testdb: connect to %s: %v", name, err)
	}
	t.Cleanup(func() {
		database.Close()
		if _, err := admin.Pool.Exec(context.Background(), "DROP DATABASE "+name); err != nil {
			t.Errorf("testdb: drop database: %v", err)
		}
		admin.Close()
	})

	if err := db.Migrate(ctx, database, store.Models()...); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}
	return database
}
