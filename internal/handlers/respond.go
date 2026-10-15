// Package handlers holds the HTTP endpoints: the embedded-app API under
// /api/admin (session-token auth), the webhook intake, App Proxy and
// server-to-server examples, and the SPA shell.
package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/admin"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/tokens"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// failAdmin maps errors from the token and Admin API layers to responses.
// A needs-reauth becomes 401: the frontend reacts by re-running the session
// bootstrap with a fresh id_token, which performs a new token exchange.
func failAdmin(w http.ResponseWriter, logger *slog.Logger, err error) {
	var gqlErrs admin.GraphQLErrors
	switch {
	case errors.Is(err, tokens.ErrNeedsReauth):
		writeError(w, http.StatusUnauthorized, "reauthorize")
	case errors.As(err, &gqlErrs):
		logger.Error("[API] GraphQL error", "error", err)
		writeError(w, http.StatusBadGateway, "shopify rejected the request")
	default:
		logger.Error("[API] Shopify call failed", "error", err)
		writeError(w, http.StatusBadGateway, "shopify unreachable")
	}
}
