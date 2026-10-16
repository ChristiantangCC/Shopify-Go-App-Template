package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/sessiontoken"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
)

// Settings reads and writes the shop's settings: the canonical "store
// something per shop" loop (form, API route, sqlc upsert) the Settings
// page demonstrates.
type Settings struct {
	Store  *store.Store
	Logger *slog.Logger
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
	if settings.NotificationEmail != "" && !strings.Contains(settings.NotificationEmail, "@") {
		writeError(w, http.StatusUnprocessableEntity, "notificationEmail must be an email address")
		return
	}

	saved, err := h.Store.SaveSettings(r.Context(), session.Shop, settings)
	if err != nil {
		h.Logger.Error("[Settings] Save failed", "shop", session.Shop, "error", err)
		writeError(w, http.StatusInternalServerError, "try again")
		return
	}
	writeJSON(w, http.StatusOK, saved)
}
