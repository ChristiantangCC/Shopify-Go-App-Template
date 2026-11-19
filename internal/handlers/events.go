package handlers

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/riverqueue/river"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/jobs"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/events"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/shopdomain"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/webhooks"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
)

// Events is the single intake for Shopify Events deliveries (the static,
// TOML-declared subscriptions; see internal/shopify/events). It follows the
// webhook intake's order: HMAC over the raw body, then one transaction that
// claims Shopify-Webhook-Id and queues the job, then 200.
type Events struct {
	Secret   string
	Store    *store.Store
	Inserter *river.Client[*sql.Tx]
	Logger   *slog.Logger
}

// ServeHTTP handles POST /events.
func (h Events) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	// Events sign exactly like webhooks: base64 HMAC-SHA256 of the raw body
	// with the client secret, in Shopify-Hmac-Sha256 (no X- prefix).
	if !webhooks.VerifyHMAC(h.Secret, body, r.Header.Get("Shopify-Hmac-Sha256")) {
		h.Logger.Warn("[Events] Rejected delivery", "reason", "bad hmac")
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	shop := r.Header.Get("Shopify-Shop-Domain")
	topic := r.Header.Get("Shopify-Topic")
	action := r.Header.Get("Shopify-Action")
	handle := r.Header.Get("Shopify-Handle")
	webhookID := r.Header.Get("Shopify-Webhook-Id")
	if !shopdomain.Valid(shop) || handle == "" || webhookID == "" {
		h.Logger.Warn("[Events] Rejected delivery", "reason", "missing headers", "shop", shop, "handle", handle)
		writeError(w, http.StatusBadRequest, "missing event headers")
		return
	}

	if _, known := events.Lookup(handle); !known {
		// 200 so Shopify doesn't retry: the subscription exists in the
		// TOML but has no Registry entry.
		h.Logger.Warn("[Events] No handler registered", "handle", handle, "topic", topic, "shop", shop)
		w.WriteHeader(http.StatusOK)
		return
	}

	duplicate, err := queueDelivery(r.Context(), h.Store, h.Inserter,
		store.WebhookDelivery{WebhookID: webhookID, ShopDomain: shop, Topic: "events/" + handle},
		jobs.EventArgs{Shop: shop, Topic: topic, Action: action, Handle: handle, WebhookID: webhookID, RequestID: middleware.GetReqID(r.Context()), Payload: body})
	if err != nil {
		h.Logger.Error("[Events] Intake failed", "error", err)
		writeError(w, http.StatusInternalServerError, "try again")
		return
	}
	if duplicate {
		h.Logger.Info("[Events] Duplicate delivery", "handle", handle, "shop", shop, "webhookId", webhookID)
		w.WriteHeader(http.StatusOK)
		return
	}
	h.Logger.Info("[Events] Delivery queued", "handle", handle, "topic", topic, "action", action, "shop", shop, "webhookId", webhookID)
	w.WriteHeader(http.StatusOK)
}
