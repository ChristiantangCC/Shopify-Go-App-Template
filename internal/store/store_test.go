package store_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/cache"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/crypto"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/exchange"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/testdb"
)

const testKey = "71f32b865b94a3618868b73f42f4bc84f5c0d04e14f84b3f9d4a16771dcf9a05"

func testToken() exchange.Token {
	return exchange.Token{
		AccessToken:           "shpat_plain_access",
		Scope:                 "write_products",
		ExpiresAt:             time.Now().Add(time.Hour),
		RefreshToken:          "refresh_plain",
		RefreshTokenExpiresAt: time.Now().Add(90 * 24 * time.Hour),
	}
}

func TestInstallLifecycle(t *testing.T) {
	database := testdb.Open(t)
	cipher, _ := crypto.New(testKey)
	shops := store.New(database.Gorm, cipher, nil)
	ctx := context.Background()
	const shop = "example-shop.myshopify.com"

	_, isNew, err := shops.SaveInstall(ctx, shop, testToken())
	if err != nil || !isNew {
		t.Fatalf("first install: isNew %v err %v", isNew, err)
	}

	install, err := shops.GetInstall(ctx, shop)
	if err != nil || install.AccessToken != "shpat_plain_access" || install.RefreshToken != "refresh_plain" {
		t.Fatalf("round trip: %+v err %v", install, err)
	}

	// The database row must hold ciphertext, never the plain tokens.
	var row store.Shop
	err = database.Gorm.Where(&store.Shop{ShopDomain: shop}).Take(&row).Error
	if err != nil || row.AccessTokenEnc == nil || row.RefreshTokenEnc == nil {
		t.Fatalf("raw row: %+v err %v", row, err)
	}
	for _, sealed := range []string{*row.AccessTokenEnc, *row.RefreshTokenEnc} {
		if !strings.HasPrefix(sealed, "v2:") || strings.Contains(sealed, "plain") {
			t.Fatalf("token stored unencrypted: %q", sealed)
		}
	}

	if _, isNew, _ := shops.SaveInstall(ctx, shop, testToken()); isNew {
		t.Fatal("re-exchange on a live install must not count as new")
	}

	if err := shops.MarkUninstalled(ctx, shop); err != nil {
		t.Fatalf("MarkUninstalled: %v", err)
	}
	install, err = shops.GetInstall(ctx, shop)
	if err != nil || !install.Uninstalled || install.AccessToken != "" || install.RefreshToken != "" {
		t.Fatalf("after uninstall: %+v err %v", install, err)
	}
	if err := shops.CheckInstalled(ctx, shop); err == nil {
		t.Fatal("CheckInstalled must fail after uninstall")
	}

	if _, isNew, _ := shops.SaveInstall(ctx, shop, testToken()); !isNew {
		t.Fatal("a reinstall after uninstall must count as new")
	}

	if err := shops.Delete(ctx, shop); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := shops.GetInstall(ctx, shop); err != store.ErrShopNotFound {
		t.Fatalf("after delete: %v, want ErrShopNotFound", err)
	}
}

func TestSettingsRoundTripAndCascade(t *testing.T) {
	database := testdb.Open(t)
	cipher, _ := crypto.New(testKey)
	shops := store.New(database.Gorm, cipher, nil)
	ctx := context.Background()
	const shop = "example-shop.myshopify.com"

	if _, _, err := shops.SaveInstall(ctx, shop, testToken()); err != nil {
		t.Fatalf("install: %v", err)
	}

	settings, err := shops.GetSettings(ctx, shop)
	if err != nil || settings.SyncEnabled != true || settings.NotificationEmail != "" {
		t.Fatalf("defaults: %+v err %v", settings, err)
	}

	saved, err := shops.SaveSettings(ctx, shop, store.Settings{NotificationEmail: "ops@example.com", SyncEnabled: false})
	if err != nil || saved.NotificationEmail != "ops@example.com" || saved.SyncEnabled {
		t.Fatalf("save: %+v err %v", saved, err)
	}
	loaded, err := shops.GetSettings(ctx, shop)
	if err != nil || loaded != saved {
		t.Fatalf("reload: %+v err %v", loaded, err)
	}

	// shop/redact deletes the shop; settings must cascade away.
	if err := shops.Delete(ctx, shop); err != nil {
		t.Fatalf("delete: %v", err)
	}
	after, err := shops.GetSettings(ctx, shop)
	if err != nil || after.NotificationEmail != "" || !after.SyncEnabled {
		t.Fatalf("after redact settings must be defaults again: %+v err %v", after, err)
	}
}

func TestClaimWebhookDeliveryDeduplicates(t *testing.T) {
	database := testdb.Open(t)
	cipher, _ := crypto.New(testKey)
	shops := store.New(database.Gorm, cipher, nil)
	ctx := context.Background()

	claim := func() bool {
		t.Helper()
		var claimed bool
		err := shops.Transaction(ctx, func(tx *gorm.DB) error {
			var err error
			claimed, err = store.ClaimWebhookDelivery(tx, store.WebhookDelivery{WebhookID: "wh-1", ShopDomain: "example-shop.myshopify.com", Topic: "products/update"})
			if err != nil {
				return err
			}
			if _, err := store.SQLTx(tx); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		return claimed
	}
	if !claim() {
		t.Fatal("first claim must succeed")
	}
	if claim() {
		t.Fatal("duplicate claim must report false")
	}

	if pruned, err := shops.PruneWebhookDeliveries(ctx, time.Now().Add(-time.Hour)); err != nil || pruned != 0 {
		t.Fatalf("prune before cutoff: rows %d err %v, want 0", pruned, err)
	}
	if pruned, err := shops.PruneWebhookDeliveries(ctx, time.Now().Add(time.Minute)); err != nil || pruned != 1 {
		t.Fatalf("prune after cutoff: rows %d err %v, want 1", pruned, err)
	}
}

func TestExpiringRefreshTokensAndLockedRefresh(t *testing.T) {
	database := testdb.Open(t)
	cipher, _ := crypto.New(testKey)
	shops := store.New(database.Gorm, cipher, nil)
	ctx := context.Background()
	const shop = "example-shop.myshopify.com"

	if _, _, err := shops.SaveInstall(ctx, shop, testToken()); err != nil {
		t.Fatalf("install: %v", err)
	}
	listed, err := shops.ListShopsWithExpiringRefreshTokens(ctx, time.Now().Add(100*24*time.Hour))
	if err != nil || len(listed) != 1 || listed[0] != shop {
		t.Fatalf("expiring: %v err %v", listed, err)
	}
	if listed, _ := shops.ListShopsWithExpiringRefreshTokens(ctx, time.Now()); len(listed) != 0 {
		t.Fatalf("nothing expires before now, got %v", listed)
	}

	refreshed := testToken()
	refreshed.AccessToken = "shpat_rotated"
	err = shops.WithShopLock(ctx, shop, func(ctx context.Context, locked store.LockedShop) error {
		if _, err := locked.Get(ctx); err != nil {
			return err
		}
		return locked.SaveTokens(ctx, refreshed)
	})
	if err != nil {
		t.Fatalf("locked refresh: %v", err)
	}
	install, err := shops.GetInstall(ctx, shop)
	if err != nil || install.AccessToken != "shpat_rotated" {
		t.Fatalf("after refresh: %+v err %v", install, err)
	}

	if err := shops.SetNeedsReauth(ctx, shop); err != nil {
		t.Fatalf("SetNeedsReauth: %v", err)
	}
	if listed, _ := shops.ListShopsWithExpiringRefreshTokens(ctx, time.Now().Add(100*24*time.Hour)); len(listed) != 0 {
		t.Fatalf("a shop needing reauth must not be listed, got %v", listed)
	}
}

// TestSettingsCacheInvalidation needs Redis too; it skips without
// TEST_REDIS_URL.
func TestSettingsCacheInvalidation(t *testing.T) {
	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("TEST_REDIS_URL not set, skipping Redis integration test")
	}
	database := testdb.Open(t)
	ctx := context.Background()
	c, err := cache.Connect(ctx, redisURL, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	cipher, _ := crypto.New(testKey)
	shops := store.New(database.Gorm, cipher, c)
	shop := fmt.Sprintf("cache-%d.myshopify.com", time.Now().UnixNano())
	t.Cleanup(func() { _ = shops.Delete(context.Background(), shop) })

	if _, _, err := shops.SaveInstall(ctx, shop, testToken()); err != nil {
		t.Fatalf("install: %v", err)
	}
	if got, _ := shops.GetSettings(ctx, shop); !got.SyncEnabled {
		t.Fatalf("defaults: %+v", got)
	}

	// Write behind the store's back: a cached read must not see it...
	direct := store.ShopSettings{ShopDomain: shop, NotificationEmail: "direct@example.com", SyncEnabled: true}
	if err := database.Gorm.Create(&direct).Error; err != nil {
		t.Fatal(err)
	}
	if got, _ := shops.GetSettings(ctx, shop); got.NotificationEmail != "" {
		t.Fatalf("read should be served from cache, got %+v", got)
	}

	// ...while a save through the store invalidates it at once.
	if _, err := shops.SaveSettings(ctx, shop, store.Settings{NotificationEmail: "ops@example.com"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := shops.GetSettings(ctx, shop); got.NotificationEmail != "ops@example.com" || got.SyncEnabled {
		t.Fatalf("after save: %+v", got)
	}
}

// TestInstallCacheInvalidation needs Redis; it skips without
// TEST_REDIS_URL. Installs are read through the cache, every write through
// the store drops the entry after commit, and the locked refresh path
// always sees the database.
func TestInstallCacheInvalidation(t *testing.T) {
	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("TEST_REDIS_URL not set, skipping Redis integration test")
	}
	database := testdb.Open(t)
	ctx := context.Background()
	c, err := cache.Connect(ctx, redisURL, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	cipher, _ := crypto.New(testKey)
	shops := store.New(database.Gorm, cipher, c)
	shop := fmt.Sprintf("install-cache-%d.myshopify.com", time.Now().UnixNano())
	t.Cleanup(func() { _ = shops.Delete(context.Background(), shop) })

	if _, _, err := shops.SaveInstall(ctx, shop, testToken()); err != nil {
		t.Fatal(err)
	}
	if got, err := shops.GetInstall(ctx, shop); err != nil || got.AccessToken != "shpat_plain_access" {
		t.Fatalf("first read: %+v err %v", got, err)
	}

	// Redis holds the sealed row, never a plain token.
	raw, err := c.Client().Get(ctx, "shop-install:"+shop).Result()
	if err != nil || strings.Contains(raw, "plain") || !strings.Contains(raw, "v2:") {
		t.Fatalf("cached entry must hold sealed tokens: %q err %v", raw, err)
	}

	// Write behind the store's back: a cached read must not see it...
	if err := database.Gorm.Model(&store.Shop{}).Where(&store.Shop{ShopDomain: shop}).Update("Scopes", "direct").Error; err != nil {
		t.Fatal(err)
	}
	if got, _ := shops.GetInstall(ctx, shop); got.Scopes == "direct" {
		t.Fatal("read should be served from cache")
	}
	// ...but the locked path reads the database.
	err = shops.WithShopLock(ctx, shop, func(ctx context.Context, locked store.LockedShop) error {
		got, err := locked.Get(ctx)
		if err == nil && got.Scopes != "direct" {
			t.Errorf("locked read must bypass the cache, got scopes %q", got.Scopes)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// A refresh under the lock invalidates the entry once it commits.
	refreshed := testToken()
	refreshed.AccessToken = "shpat_refreshed"
	err = shops.WithShopLock(ctx, shop, func(ctx context.Context, locked store.LockedShop) error {
		return locked.SaveTokens(ctx, refreshed)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := shops.GetInstall(ctx, shop); got.AccessToken != "shpat_refreshed" {
		t.Fatalf("after locked refresh: %+v", got)
	}

	// Uninstall invalidates too, so the App Proxy stops accepting the shop.
	if err := shops.MarkUninstalled(ctx, shop); err != nil {
		t.Fatal(err)
	}
	if err := shops.CheckInstalled(ctx, shop); err == nil {
		t.Fatal("uninstalled shop must fail CheckInstalled")
	}
}

func TestResealTokensMovesShopsToTheCurrentKey(t *testing.T) {
	database := testdb.Open(t)
	ctx := context.Background()
	const oldKey = "0000000000000000000000000000000000000000000000000000000000000001"
	const shop = "rotate-shop.myshopify.com"

	before, _ := crypto.New(oldKey)
	if _, _, err := store.New(database.Gorm, before, nil).SaveInstall(ctx, shop, testToken()); err != nil {
		t.Fatal(err)
	}

	after, _ := crypto.New(testKey, oldKey)
	shops := store.New(database.Gorm, after, nil)
	stale, err := shops.ListShopsWithStaleTokens(ctx, 0, 10)
	if err != nil || len(stale) != 1 || stale[0].ShopDomain != shop {
		t.Fatalf("stale before reseal: %+v err %v", stale, err)
	}

	changed, err := shops.ResealTokens(ctx, shop)
	if err != nil || !changed {
		t.Fatalf("reseal: changed %v err %v", changed, err)
	}
	if again, _ := shops.ResealTokens(ctx, shop); again {
		t.Fatal("a second reseal must find nothing to do")
	}
	if stale, _ := shops.ListShopsWithStaleTokens(ctx, 0, 10); len(stale) != 0 {
		t.Fatalf("stale after reseal: %+v", stale)
	}

	// The tokens survive, and no longer need the old key.
	current, _ := crypto.New(testKey)
	install, err := store.New(database.Gorm, current, nil).GetInstall(ctx, shop)
	if err != nil || install.AccessToken != "shpat_plain_access" || install.RefreshToken != "refresh_plain" {
		t.Fatalf("after reseal, without the old key: %+v err %v", install, err)
	}
}
