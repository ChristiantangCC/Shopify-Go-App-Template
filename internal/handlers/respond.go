// Package handlers holds the HTTP endpoints: the typed JSON API (api.go,
// huma: /api/admin with session-token auth and /api/service with API
// keys), the webhook and Events intakes, App Proxy routes, health probes
// and the SPA shell.
package handlers

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
