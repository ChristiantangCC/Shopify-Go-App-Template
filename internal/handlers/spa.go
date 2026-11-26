package handlers

import (
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/shopdomain"
)

// SPA serves the embedded admin frontend. Every HTML response carries a
// frame-ancestors CSP so only the shop's admin and admin.shopify.com can
// frame the app; the shop comes from the query string and is validated
// before it reaches the header. With DevURL set, requests proxy to the Vite
// dev server instead, for hot reload.
type SPA struct {
	// Dist is the built frontend (web/frontend/dist).
	Dist fs.FS
	// APIKey is the app's client id, injected into index.html for App Bridge.
	APIKey string
	// DevURL, when non-empty, proxies to a running Vite dev server.
	DevURL string
	Logger *slog.Logger

	index []byte
	proxy *httputil.ReverseProxy
}

// NewSPA prepares the handler: it loads index.html once and injects the
// App Bridge api key.
func NewSPA(dist fs.FS, apiKey, devURL string, logger *slog.Logger) (*SPA, error) {
	spa := &SPA{Dist: dist, APIKey: apiKey, DevURL: devURL, Logger: logger}
	if devURL != "" {
		target, err := url.Parse(devURL)
		if err != nil {
			return nil, err
		}
		// SetURL also rewrites the Host header to the target's: Vite only
		// answers hosts it knows, so the request arrives as a localhost one
		// instead of the tunnel domain.
		spa.proxy = &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
		}}
		return spa, nil
	}
	raw, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		return nil, err
	}
	spa.index = []byte(strings.ReplaceAll(string(raw), "__SHOPIFY_API_KEY__", apiKey))
	return spa, nil
}

// ServeHTTP serves static assets as they are and index.html for everything
// else, so client-side routes deep-link correctly.
func (h *SPA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", frameAncestors(r.URL.Query().Get("shop")))

	if h.proxy != nil {
		// Page navigations go through devIndex so the api key gets
		// injected exactly like in production; everything else (modules
		// like /@vite/client and /src/main.tsx, assets, HMR) proxies
		// straight through to Vite.
		if r.Method == http.MethodGet && isDocumentRequest(r) {
			h.devIndex(w, r)
			return
		}
		h.proxy.ServeHTTP(w, r)
		return
	}

	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name != "" && name != "index.html" {
		if file, err := h.Dist.Open(name); err == nil {
			_ = file.Close()
			// Vite builds hashed asset names, safe to cache hard.
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.FileServerFS(h.Dist).ServeHTTP(w, r)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(h.index)
}

// isDocumentRequest reports whether the browser is navigating to a page,
// as opposed to fetching a module, asset or API response.
func isDocumentRequest(r *http.Request) bool {
	if dest := r.Header.Get("Sec-Fetch-Dest"); dest != "" {
		return dest == "document" || dest == "iframe"
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// devClient fetches index.html from Vite. The timeout keeps a hung dev
// server from holding the request open.
var devClient = &http.Client{Timeout: 5 * time.Second}

// devIndex serves Vite's transformed index.html with the api key injected.
func (h *SPA) devIndex(w http.ResponseWriter, r *http.Request) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.DevURL+"/index.html", nil)
	if err != nil {
		http.Error(w, "bad FRONTEND_DEV_URL", http.StatusInternalServerError)
		return
	}
	res, err := devClient.Do(req)
	if err != nil {
		h.Logger.Error("[SPA] Vite dev server unreachable", "url", h.DevURL, "error", err)
		http.Error(w, "vite dev server unreachable, is `bun run dev` running?", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		http.Error(w, "vite dev server unreachable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(strings.ReplaceAll(string(raw), "__SHOPIFY_API_KEY__", h.APIKey)))
}

// frameAncestors builds the CSP value. An invalid or missing shop parameter
// falls back to allowing any shop's admin, never to allowing everything.
func frameAncestors(shop string) string {
	if shopdomain.Valid(shop) {
		return "frame-ancestors https://" + shop + " https://admin.shopify.com;"
	}
	return "frame-ancestors https://*.myshopify.com https://admin.shopify.com;"
}
