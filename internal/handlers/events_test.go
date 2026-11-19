package handlers_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/crypto"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/handlers"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/jobs"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/events"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/testdb"
)

const secret = "test-secret"

func sign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestEventsIntake(t *testing.T) {
	database := testdb.Open(t)
	logger := slog.New(slog.DiscardHandler)
	cipher, _ := crypto.New("71f32b865b94a3618868b73f42f4bc84f5c0d04e14f84b3f9d4a16771dcf9a05")
	inserter, err := jobs.NewInserter(database.SQL, logger)
	if err != nil {
		t.Fatal(err)
	}
	intake := handlers.Events{Secret: secret, Store: store.New(database.Gorm, cipher, nil), Inserter: inserter, Logger: logger}

	events.Registry["test-handle"] = func(context.Context, events.Deps, events.Delivery) error { return nil }
	t.Cleanup(func() { delete(events.Registry, "test-handle") })

	body := []byte(`{"topic":"Product","action":"update","handle":"test-handle","data":{"product":{"id":"gid://shopify/Product/1"}},"fields_changed":{"added":[],"updated":[],"removed":[]},"query_variables":{}}`)
	post := func(handle, webhookID, signature string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
		req.Header.Set("Shopify-Hmac-Sha256", signature)
		req.Header.Set("Shopify-Shop-Domain", "example-shop.myshopify.com")
		req.Header.Set("Shopify-Topic", "Product")
		req.Header.Set("Shopify-Action", "update")
		req.Header.Set("Shopify-Handle", handle)
		req.Header.Set("Shopify-Webhook-Id", webhookID)
		rec := httptest.NewRecorder()
		intake.ServeHTTP(rec, req)
		return rec.Code
	}
	jobCount := func() int64 {
		t.Helper()
		var n int64
		if err := database.Gorm.Table("river_job").Where(map[string]any{"kind": jobs.EventArgs{}.Kind()}).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}

	if code := post("test-handle", "ev-1", "bm90LXRoZS1zaWduYXR1cmU="); code != http.StatusUnauthorized || jobCount() != 0 {
		t.Fatalf("forged delivery: code %d jobs %d", code, jobCount())
	}
	if code := post("test-handle", "ev-1", sign(body)); code != http.StatusOK || jobCount() != 1 {
		t.Fatalf("first delivery: code %d jobs %d", code, jobCount())
	}
	if code := post("test-handle", "ev-1", sign(body)); code != http.StatusOK || jobCount() != 1 {
		t.Fatalf("retried delivery must not queue twice: code %d jobs %d", code, jobCount())
	}
	if code := post("unknown-handle", "ev-2", sign(body)); code != http.StatusOK || jobCount() != 1 {
		t.Fatalf("unknown handle is acknowledged without a job: code %d jobs %d", code, jobCount())
	}
}
