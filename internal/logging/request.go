package logging

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// RequestLogger logs one line per API request: method, path, status,
// duration and the request id, which the webhook intake also stamps onto
// its jobs so a request and its background work share an id in the logs.
// Page and asset traffic stays out; only the paths that do work are worth
// a line.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	logged := []string{"/api/", "/webhooks", "/proxy"}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			worthLogging := false
			for _, prefix := range logged {
				if strings.HasPrefix(r.URL.Path, prefix) {
					worthLogging = true
					break
				}
			}
			if !worthLogging {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(wrapped, r)
			logger.Info("[HTTP] "+r.Method+" "+r.URL.Path,
				"status", wrapped.Status(),
				"ms", time.Since(start).Milliseconds(),
				"req", ShortID(middleware.GetReqID(r.Context())),
			)
		})
	}
}
