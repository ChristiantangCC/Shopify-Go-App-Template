package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/crypto"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/exchange"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
	storegen "github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store/gen"
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
	pool := testdb.Pool(t)
	cipher, _ := crypto.New(testKey)
	shops := store.New(pool, cipher)
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
	row, err := storegen.New(pool).GetShop(ctx, shop)
	if err != nil || row.AccessTokenEnc == nil || row.RefreshTokenEnc == nil {
		t.Fatalf("raw row: %+v err %v", row, err)
	}
	for _, sealed := range []string{*row.AccessTokenEnc, *row.RefreshTokenEnc} {
		if !strings.HasPrefix(sealed, "v1:") || strings.Contains(sealed, "plain") {
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

func TestClaimWebhookDeliveryDeduplicates(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	queries := storegen.New(pool)

	params := storegen.ClaimWebhookDeliveryParams{WebhookID: "wh-1", ShopDomain: "example-shop.myshopify.com", Topic: "products/update"}
	first, err := queries.ClaimWebhookDelivery(ctx, params)
	if err != nil || first != 1 {
		t.Fatalf("first claim: rows %d err %v", first, err)
	}
	second, err := queries.ClaimWebhookDelivery(ctx, params)
	if err != nil || second != 0 {
		t.Fatalf("duplicate claim: rows %d err %v, want 0 rows", second, err)
	}
}
