package handlers

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/jobs"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/shopdomain"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/webhooks"
	storegen "github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store/gen"
)

// Webhook is the single intake for every topic. Order matters:
//
//  1. HMAC over the raw body; a forged request gets 401 and can't touch
//     the dedupe table.
//  2. One transaction claims the delivery id and inserts the job. Commit,
//     then 200. A duplicate delivery conflicts on the claim and still gets
//     200; a failed enqueue rolls the claim back and answers 500, so
//     Shopify retries and nothing is ever lost after a 200.
//
// Handlers run in River workers; this endpoint never does topic work.
type Webhook struct {
	Secret string
	Pool   *pgxpool.Pool
	River  *river.Client[pgx.Tx]
	Logger *slog.Logger
}

// ServeHTTP handles POST /webhooks.
func (h Webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	if !webhooks.VerifyHMAC(h.Secret, body, r.Header.Get("X-Shopify-Hmac-Sha256")) {
		h.Logger.Warn("[Webhooks] Rejected delivery", "reason", "bad hmac")
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	shop := r.Header.Get("X-Shopify-Shop-Domain")
	topic := r.Header.Get("X-Shopify-Topic")
	webhookID := r.Header.Get("X-Shopify-Webhook-Id")
	if !shopdomain.Valid(shop) || topic == "" || webhookID == "" {
		h.Logger.Warn("[Webhooks] Rejected delivery", "reason", "missing headers", "shop", shop, "topic", topic)
		writeError(w, http.StatusBadRequest, "missing webhook headers")
		return
	}
	if _, known := webhooks.Lookup(topic); !known {
		// Still 200: anything else makes Shopify retry a topic nobody
		// handles. Hitting this means a subscription exists without a
		// registry entry.
		h.Logger.Warn("[Webhooks] No handler registered", "topic", topic, "shop", shop)
		w.WriteHeader(http.StatusOK)
		return
	}

	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		h.Logger.Error("[Webhooks] Intake failed", "error", err)
		writeError(w, http.StatusInternalServerError, "try again")
		return
	}
	defer tx.Rollback(r.Context())

	claimed, err := storegen.New(tx).ClaimWebhookDelivery(r.Context(), storegen.ClaimWebhookDeliveryParams{
		WebhookID:  webhookID,
		ShopDomain: shop,
		Topic:      topic,
	})
	if err != nil {
		h.Logger.Error("[Webhooks] Claim failed", "error", err)
		writeError(w, http.StatusInternalServerError, "try again")
		return
	}
	if claimed == 0 {
		h.Logger.Info("[Webhooks] Duplicate delivery", "topic", topic, "shop", shop, "webhookId", webhookID)
		w.WriteHeader(http.StatusOK)
		return
	}

	_, err = h.River.InsertTx(r.Context(), tx, jobs.WebhookArgs{Shop: shop, Topic: topic, WebhookID: webhookID, Payload: body}, nil)
	if err != nil {
		h.Logger.Error("[Webhooks] Enqueue failed", "error", err)
		writeError(w, http.StatusInternalServerError, "try again")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.Logger.Error("[Webhooks] Commit failed", "error", err)
		writeError(w, http.StatusInternalServerError, "try again")
		return
	}

	h.Logger.Info("[Webhooks] Delivery queued", "topic", topic, "shop", shop, "webhookId", webhookID)
	w.WriteHeader(http.StatusOK)
}
