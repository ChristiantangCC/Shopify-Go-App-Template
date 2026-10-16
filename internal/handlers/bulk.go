package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/admin"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/bulk"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/sessiontoken"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/tokens"
)

// StartProductsBulk starts the demo bulk export: every product's id and
// title. Shopify delivers bulk_operations/finish when it is done and the
// registry handler downloads the result.
type StartProductsBulk struct {
	Admin  *admin.Client
	Logger *slog.Logger
}

// ServeHTTP handles POST /api/admin/bulk/products.
func (h StartProductsBulk) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	session, ok := sessiontoken.SessionFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	operationID, err := bulk.Start(r.Context(), h.Admin, session.Shop, bulk.ProductsQuery)
	if err != nil {
		if errors.Is(err, tokens.ErrNeedsReauth) {
			failAdmin(w, h.Logger, err)
			return
		}
		// Usually "a bulk query is already in progress"; tell the caller.
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.Logger.Info("[Bulk] Export started", "shop", session.Shop, "operation", operationID)
	writeJSON(w, http.StatusAccepted, map[string]string{"operationId": operationID})
}
