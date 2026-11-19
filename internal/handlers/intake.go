package handlers

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/riverqueue/river"
	"gorm.io/gorm"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
)

// queueDelivery claims a delivery id and inserts its job in one
// transaction, shared by the webhook and Events intakes. It reports
// duplicate when the id was already claimed (nothing is inserted then). On
// error nothing is committed, so the caller answers 5xx and Shopify retries.
func queueDelivery(ctx context.Context, shops *store.Store, inserter *river.Client[*sql.Tx], delivery store.WebhookDelivery, args river.JobArgs) (duplicate bool, err error) {
	err = shops.Transaction(ctx, func(tx *gorm.DB) error {
		claimed, err := store.ClaimWebhookDelivery(tx, delivery)
		if err != nil {
			return fmt.Errorf("claim: %w", err)
		}
		if !claimed {
			duplicate = true
			return nil
		}
		sqlTx, err := store.SQLTx(tx)
		if err != nil {
			return err
		}
		if _, err := inserter.InsertTx(ctx, sqlTx, args, nil); err != nil {
			return fmt.Errorf("enqueue: %w", err)
		}
		return nil
	})
	return duplicate, err
}
