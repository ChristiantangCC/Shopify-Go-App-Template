package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/cache"
)

// Health answers 200 when the app, its database and Redis are up. Point
// uptime monitoring at it.
type Health struct {
	Pool  *pgxpool.Pool
	Cache *cache.Cache
}

// ServeHTTP handles GET /health.
func (h Health) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.Pool.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unreachable")
		return
	}
	if err := h.Cache.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "redis unreachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
