package jobs

import (
	"context"
	"log/slog"
	"testing"

	"github.com/riverqueue/river"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/crypto"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/exchange"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/testdb"
)

// A shop sealed with a key that is no longer configured can't be
// re-encrypted; the job must log it and still finish the others.
func TestReencryptSkipsShopsItCannotOpen(t *testing.T) {
	database := testdb.Open(t)
	ctx := context.Background()
	const (
		currentKey = "71f32b865b94a3618868b73f42f4bc84f5c0d04e14f84b3f9d4a16771dcf9a05"
		oldKey     = "0000000000000000000000000000000000000000000000000000000000000001"
		lostKey    = "0000000000000000000000000000000000000000000000000000000000000002"
	)
	token := exchange.Token{AccessToken: "shpat_x", RefreshToken: "refresh_x"}
	lost, _ := crypto.New(lostKey)
	old, _ := crypto.New(oldKey)
	// The unreadable shop gets the lower id, so it is listed first.
	if _, _, err := store.New(database.Gorm, lost, nil).SaveInstall(ctx, "lost.myshopify.com", token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.New(database.Gorm, old, nil).SaveInstall(ctx, "old.myshopify.com", token); err != nil {
		t.Fatal(err)
	}

	cipher, _ := crypto.New(currentKey, oldKey)
	shops := store.New(database.Gorm, cipher, nil)
	worker := &ReencryptTokensWorker{store: shops, keyID: cipher.CurrentKeyID(), logger: slog.New(slog.DiscardHandler)}
	if err := worker.Work(ctx, &river.Job[ReencryptTokensArgs]{}); err != nil {
		t.Fatal(err)
	}

	stale, err := shops.ListShopsWithStaleTokens(ctx, 0, 10)
	if err != nil || len(stale) != 1 || stale[0].ShopDomain != "lost.myshopify.com" {
		t.Fatalf("only the unreadable shop should stay stale: %+v err %v", stale, err)
	}
}
