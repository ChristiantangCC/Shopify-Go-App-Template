package handlers

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/cache"
)

// Probes answers the orchestrator's health checks:
//
//   - GET /livez: the process is up and serving HTTP. It checks no
//     dependency, so a database outage never gets the app restarted in a
//     loop.
//   - GET /readyz: the instance should receive traffic: Postgres answers
//     and the server is not shutting down. Redis is reported but never
//     makes the app unready, because every Redis use falls back.
//   - GET /health: the same as /readyz, kept for uptime monitors.
type Probes struct {
	Pool  *pgxpool.Pool
	Cache *cache.Cache

	draining atomic.Bool
}

// Drain marks the instance as shutting down: /readyz answers 503 from now
// on, so load balancers stop sending it new requests.
func (p *Probes) Drain() { p.draining.Store(true) }

// Live handles GET /livez.
func (p *Probes) Live(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready handles GET /readyz and GET /health.
func (p *Probes) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	ready := !p.draining.Load()
	database := "ok"
	if err := p.Pool.Ping(ctx); err != nil {
		database = "unreachable"
		ready = false
	}
	redis := "disabled"
	if p.Cache.Client() != nil {
		redis = "ok"
		if err := p.Cache.Ping(ctx); err != nil {
			redis = "unreachable"
		}
	}

	status, code := "ok", http.StatusOK
	if !ready {
		status, code = "unavailable", http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"status":   status,
		"database": database,
		"redis":    redis,
		"draining": p.draining.Load(),
	})
}
