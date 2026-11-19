package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"gorm.io/gorm"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/jobs"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/testdb"
)

// The webhook intake depends on this: a job inserted through the
// insert-only client commits and rolls back with the GORM transaction.
func TestInserterJoinsTheGormTransaction(t *testing.T) {
	database := testdb.Open(t)
	ctx := context.Background()
	inserter, err := jobs.NewInserter(database.SQL, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	count := func() int64 {
		t.Helper()
		var n int64
		if err := database.Gorm.Table("river_job").Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	insert := func(tx *gorm.DB) error {
		sqlTx, err := store.SQLTx(tx)
		if err != nil {
			return err
		}
		_, err = inserter.InsertTx(ctx, sqlTx, jobs.SyncWebhooksArgs{Shop: "example-shop.myshopify.com"}, nil)
		return err
	}

	rollback := errors.New("rollback")
	err = database.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := insert(tx); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) || count() != 0 {
		t.Fatalf("rolled back insert must leave no job: err %v jobs %d", err, count())
	}

	if err := database.Gorm.WithContext(ctx).Transaction(insert); err != nil {
		t.Fatal(err)
	}
	if count() != 1 {
		t.Fatalf("committed insert must leave one job, got %d", count())
	}
}
