package db_test

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"testing"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/db"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/testdb"
)

// TestMigrationsMatchModels builds one database from the SQL migrations
// and one from the GORM models (AutoMigrate), then compares their columns,
// indexes and constraints. A model change without a matching migration, or
// the reverse, fails here.
func TestMigrationsMatchModels(t *testing.T) {
	ctx := context.Background()
	migrated := testdb.Open(t)
	modelled := testdb.OpenEmpty(t)
	if err := modelled.Gorm.WithContext(ctx).AutoMigrate(store.Models()...); err != nil {
		t.Fatal(err)
	}

	want := describe(t, modelled)
	got := describe(t, migrated)
	for _, line := range want {
		if !slices.Contains(got, line) {
			t.Errorf("models have, migrations don't: %s", line)
		}
	}
	for _, line := range got {
		if !slices.Contains(want, line) {
			t.Errorf("migrations have, models don't: %s", line)
		}
	}
}

// TestMigrateAdoptsAnAutoMigratedDatabase covers databases created before
// migrations were versioned: the initial migration must apply cleanly on
// top of the schema AutoMigrate built.
func TestMigrateAdoptsAnAutoMigratedDatabase(t *testing.T) {
	ctx := context.Background()
	database := testdb.OpenEmpty(t)
	if err := database.Gorm.WithContext(ctx).AutoMigrate(store.Models()...); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, database, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("migrate on an AutoMigrate schema: %v", err)
	}
}

// describe lists the app tables' columns, indexes and constraints as
// comparable lines. River's and goose's own tables are left out.
func describe(t *testing.T, database *db.DB) []string {
	t.Helper()
	queries := []string{
		`SELECT format('column %s.%s %s null=%s default=%s', table_name, column_name, data_type, is_nullable, coalesce(column_default, ''))
		   FROM information_schema.columns
		  WHERE table_schema = 'public' AND table_name NOT LIKE 'river%' AND table_name NOT LIKE 'goose%'`,
		`SELECT format('index %s %s', indexname, indexdef)
		   FROM pg_indexes
		  WHERE schemaname = 'public' AND tablename NOT LIKE 'river%' AND tablename NOT LIKE 'goose%'`,
		`SELECT format('constraint %s.%s %s', rel.relname, con.conname, pg_get_constraintdef(con.oid))
		   FROM pg_constraint con JOIN pg_class rel ON rel.oid = con.conrelid
		   JOIN pg_namespace ns ON ns.oid = rel.relnamespace
		  WHERE ns.nspname = 'public' AND rel.relname NOT LIKE 'river%' AND rel.relname NOT LIKE 'goose%'
		    AND con.contype IN ('p', 'u', 'f', 'c')`,
	}
	var lines []string
	for _, query := range queries {
		rows, err := database.Pool.Query(context.Background(), query)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, line)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	if len(lines) == 0 {
		t.Fatal(fmt.Errorf("no schema found"))
	}
	slices.Sort(lines)
	return lines
}
