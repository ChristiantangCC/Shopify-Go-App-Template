package handlers_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/crypto"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/handlers"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/jobs"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/testdb"
)

func TestWebhookIntake(t *testing.T) {
	database := testdb.Open(t)
	logger := slog.New(slog.DiscardHandler)
	cipher, _ := crypto.New("71f32b865b94a3618868b73f42f4bc84f5c0d04e14f84b3f9d4a16771dcf9a05")
	inserter, err := jobs.NewInserter(database.SQL, logger)
	if err != nil {
		t.Fatal(err)
	}
	intake := handlers.Webhook{Secret: secret, Store: store.New(database.Gorm, cipher, nil), Inserter: inserter, Logger: logger}

	body := []byte(`{"id":1,"title":"Shirt"}`)
	post := func(shop, topic, webhookID, signature string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/webhooks", bytes.NewReader(body))
		req.Header.Set("X-Shopify-Hmac-Sha256", signature)
		req.Header.Set("X-Shopify-Shop-Domain", shop)
		req.Header.Set("X-Shopify-Topic", topic)
		req.Header.Set("X-Shopify-Webhook-Id", webhookID)
		rec := httptest.NewRecorder()
		intake.ServeHTTP(rec, req)
		return rec.Code
	}
	jobCount := func() int64 {
		t.Helper()
		var n int64
		if err := database.Gorm.Table("river_job").Where(map[string]any{"kind": jobs.WebhookArgs{}.Kind()}).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	const shop = "example-shop.myshopify.com"

	if code := post(shop, "products/create", "wh-1", "bm90LXRoZS1zaWduYXR1cmU="); code != http.StatusUnauthorized || jobCount() != 0 {
		t.Fatalf("forged delivery: code %d jobs %d", code, jobCount())
	}
	if code := post("evil.example.com", "products/create", "wh-1", sign(body)); code != http.StatusBadRequest || jobCount() != 0 {
		t.Fatalf("non-myshopify shop: code %d jobs %d", code, jobCount())
	}
	if code := post(shop, "products/create", "", sign(body)); code != http.StatusBadRequest || jobCount() != 0 {
		t.Fatalf("missing webhook id: code %d jobs %d", code, jobCount())
	}
	if code := post(shop, "products/create", "wh-1", sign(body)); code != http.StatusOK || jobCount() != 1 {
		t.Fatalf("first delivery: code %d jobs %d", code, jobCount())
	}
	if code := post(shop, "products/create", "wh-1", sign(body)); code != http.StatusOK || jobCount() != 1 {
		t.Fatalf("retried delivery must not queue twice: code %d jobs %d", code, jobCount())
	}
	if code := post(shop, "carts/update", "wh-2", sign(body)); code != http.StatusOK || jobCount() != 1 {
		t.Fatalf("unregistered topic is acknowledged without a job: code %d jobs %d", code, jobCount())
	}
}
