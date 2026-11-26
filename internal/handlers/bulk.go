package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/admin"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/bulk"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/sessiontoken"
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
	var userErr *bulk.UserError
	switch {
	case errors.As(err, &userErr):
		// Usually "a bulk query is already in progress": Shopify's own
		// message, written for the merchant.
		writeError(w, http.StatusUnprocessableEntity, userErr.Message)
		return
	case err != nil:
		// Anything else stays in the logs; failAdmin answers generically.
		failAdmin(w, h.Logger, err)
		return
	}
	h.Logger.Info("[Bulk] Export started", "shop", session.Shop, "operation", operationID)
	writeJSON(w, http.StatusAccepted, map[string]string{"operationId": operationID})
}
