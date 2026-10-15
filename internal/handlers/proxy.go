package handlers

import (
	"net/http"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/appproxy"
)

// ProxyPing is the App Proxy example: the storefront calls
// /apps/<subpath>/ping and Shopify forwards it here, signed. Replace it
// with real storefront endpoints; appproxy.CallerFrom tells you the shop
// and the logged-in customer.
type ProxyPing struct{}

// ServeHTTP handles GET /proxy/ping.
func (h ProxyPing) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	caller, ok := appproxy.CallerFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"shop":       caller.Shop,
		"customerId": caller.CustomerID,
		"loggedIn":   caller.CustomerID != "",
	})
}
