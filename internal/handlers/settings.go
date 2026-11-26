package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/jobs"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/sessiontoken"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
)

// Settings reads and writes the shop's settings: the canonical "store
// something per shop" loop (form, API route, GORM upsert, Redis-cached
// read) the Settings page demonstrates.
type Settings struct {
	Store *store.Store
	// River queues a webhook re-sync after a save, because dynamic
	// subscriptions can depend on settings (see the registry's When).
	River *river.Client[pgx.Tx]
	// Host is the app's public base URL, passed on to the sync job.
	Host   string
	Logger *slog.Logger
}

// validEmail accepts a bare address ("ops@example.com"), not a display-name
// form like "Ops <ops@example.com>": the value is used as a recipient as is.
func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value
}

// Get handles GET /api/admin/settings.
func (h Settings) Get(w http.ResponseWriter, r *http.Request) {
	session, ok := sessiontoken.SessionFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	settings, err := h.Store.GetSettings(r.Context(), session.Shop)
	if err != nil {
		h.Logger.Error("[Settings] Read failed", "shop", session.Shop, "error", err)
		writeError(w, http.StatusInternalServerError, "try again")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// Put handles PUT /api/admin/settings. Validation lives here, server-side;
// the form only mirrors it for friendliness.
func (h Settings) Put(w http.ResponseWriter, r *http.Request) {
	session, ok := sessiontoken.SessionFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var settings store.Settings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&settings); err != nil {
		writeError(w, http.StatusBadRequest, "bad body")
		return
	}
	settings.NotificationEmail = strings.TrimSpace(settings.NotificationEmail)
	if settings.NotificationEmail != "" && !validEmail(settings.NotificationEmail) {
		writeError(w, http.StatusUnprocessableEntity, "notificationEmail must be an email address")
		return
	}

	saved, err := h.Store.SaveSettings(r.Context(), session.Shop, settings)
	if err != nil {
		h.Logger.Error("[Settings] Save failed", "shop", session.Shop, "error", err)
		writeError(w, http.StatusInternalServerError, "try again")
		return
	}
	if _, err := h.River.Insert(r.Context(), jobs.SyncWebhooksArgs{Shop: session.Shop, Host: h.Host}, nil); err != nil {
		// The save stands; the next app open re-syncs anyway.
		h.Logger.Warn("[Settings] Could not queue webhook sync", "shop", session.Shop, "error", err)
	}
	writeJSON(w, http.StatusOK, saved)
}
