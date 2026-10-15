package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/sessiontoken"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/webhooks"
)

// WebhookSubscriptions powers the home page's live subscribe/unsubscribe
// demo for dynamic topics. Only topics in the registry are allowed, so the
// endpoint can't be used to point arbitrary subscriptions at the app.
type WebhookSubscriptions struct {
	Subscriber *webhooks.Subscriber
	Logger     *slog.Logger
}

// List handles GET: the subscription state of every dynamic topic.
func (h WebhookSubscriptions) List(w http.ResponseWriter, r *http.Request) {
	session, ok := sessiontoken.SessionFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	status, err := h.Subscriber.Subscribed(r.Context(), session.Shop)
	if err != nil {
		failAdmin(w, h.Logger, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"topics": status})
}

// Toggle handles POST {topic, subscribed}: it creates or deletes the
// shop's subscription for one registry topic.
func (h WebhookSubscriptions) Toggle(w http.ResponseWriter, r *http.Request) {
	session, ok := sessiontoken.SessionFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Topic      string `json:"topic"`
		Subscribed bool   `json:"subscribed"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad body")
		return
	}
	registration, known := webhooks.Lookup(body.Topic)
	if !known || registration.Source != webhooks.SourceDynamic {
		writeError(w, http.StatusBadRequest, "not a dynamic registry topic")
		return
	}

	var err error
	if body.Subscribed {
		err = h.Subscriber.Subscribe(r.Context(), session.Shop, body.Topic)
	} else {
		err = h.Subscriber.Unsubscribe(r.Context(), session.Shop, body.Topic)
	}
	if err != nil {
		failAdmin(w, h.Logger, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"topic": body.Topic, "subscribed": body.Subscribed})
}
