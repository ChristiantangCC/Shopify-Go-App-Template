package handlers

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/jobs"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/sessiontoken"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/tokens"
)

// Session bootstraps a shop: the frontend POSTs here on every app open. It
// runs a token exchange only when the shop has no usable offline token, and
// queues a webhook-subscription sync so dynamic topics follow the registry
// (and the callback URL follows a changed dev tunnel).
type Session struct {
	Tokens         *tokens.Service
	River          *river.Client[pgx.Tx]
	BillingEnabled bool
	Logger         *slog.Logger
}

// ServeHTTP handles POST /api/admin/session.
func (h Session) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	session, ok := sessiontoken.SessionFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	isNew, err := h.Tokens.EnsureInstall(r.Context(), session.Shop, session.Token)
	if err != nil {
		h.Logger.Error("[Session] Install failed", "shop", session.Shop, "error", err)
		writeError(w, http.StatusBadGateway, "token exchange failed")
		return
	}
	if isNew {
		h.Logger.Info("[Session] App installed", "shop", session.Shop)
	}

	if _, err := h.River.Insert(r.Context(), jobs.SyncWebhooksArgs{Shop: session.Shop}, nil); err != nil {
		// The app still works without the sync; the next open retries it.
		h.Logger.Warn("[Session] Could not queue webhook sync", "shop", session.Shop, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"shop":           session.Shop,
		"isNew":          isNew,
		"billingEnabled": h.BillingEnabled,
	})
}
