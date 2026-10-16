// The server binary runs everything: HTTP (embedded admin UI, its API, the
// webhook intake, App Proxy and server-to-server routes) plus the River
// workers that process webhooks and scheduled jobs. One process, one
// Postgres, nothing else.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/apikey"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/appproxy"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/config"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/crypto"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/db"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/handlers"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/jobs"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/logging"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/admin"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/exchange"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/sessiontoken"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/tokens"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/webhooks"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := logging.New(logging.Options{Level: cfg.LogLevel, Format: cfg.LogFormat})

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	logger.Info("[Server] Database ready")

	cipher, err := crypto.New(cfg.EncryptionKey)
	if err != nil {
		return err
	}
	shops := store.New(pool, cipher)
	tokenService := tokens.New(shops, exchange.New(cfg.ClientID, cfg.ClientSecret), logger)
	adminClient := admin.New(cfg.APIVersion, tokenService)
	subscriber := webhooks.NewSubscriber(adminClient, cfg.Host, logger)

	riverClient, err := jobs.NewClient(jobs.Options{
		Pool:       pool,
		Store:      shops,
		Admin:      adminClient,
		Tokens:     tokenService,
		Subscriber: subscriber,
		Logger:     logger,
	})
	if err != nil {
		return err
	}
	if err := riverClient.Start(ctx); err != nil {
		return fmt.Errorf("jobs: %w", err)
	}
	logger.Info("[Server] Workers running")

	dist, err := web.Dist()
	if err != nil {
		return err
	}
	spa, err := handlers.NewSPA(dist, cfg.ClientID, cfg.FrontendDevURL, logger)
	if err != nil {
		return err
	}

	router := chi.NewRouter()
	router.Use(middleware.RequestID, logging.RequestLogger(logger), middleware.Recoverer, middleware.Timeout(30*time.Second))

	router.Method(http.MethodGet, "/health", handlers.Health{Pool: pool})
	router.Method(http.MethodPost, "/webhooks", handlers.Webhook{Secret: cfg.ClientSecret, Pool: pool, River: riverClient, Logger: logger})

	// Embedded admin API: every route behind session-token verification.
	router.Route("/api/admin", func(r chi.Router) {
		r.Use(sessiontoken.Middleware(cfg.ClientID, cfg.ClientSecret, logger))
		r.Method(http.MethodPost, "/session", handlers.Session{Tokens: tokenService, River: riverClient, BillingEnabled: cfg.BillingEnabled, Logger: logger})
		r.Method(http.MethodGet, "/products", handlers.ListProducts{Admin: adminClient, Shop: shopFromSession, Logger: logger})
		webhookSubs := handlers.WebhookSubscriptions{Subscriber: subscriber, Logger: logger}
		r.Get("/webhooks/subscriptions", webhookSubs.List)
		r.Post("/webhooks/subscriptions", webhookSubs.Toggle)
		settings := handlers.Settings{Store: shops, Logger: logger}
		r.Get("/settings", settings.Get)
		r.Put("/settings", settings.Put)
		r.Method(http.MethodPost, "/bulk/products", handlers.StartProductsBulk{Admin: adminClient, Logger: logger})
		if cfg.BillingEnabled {
			r.Method(http.MethodGet, "/billing/status", handlers.BillingStatus{Admin: adminClient, AppHandle: cfg.AppHandle, Logger: logger})
		}
	})

	// App Proxy: signed storefront requests, rate limited per caller.
	router.Route("/proxy", func(r chi.Router) {
		r.Use(appproxy.Verify(appproxy.VerifyOptions{Secret: cfg.ClientSecret, MaxAge: cfg.ProxyMaxAge, Logger: logger, CheckShop: shops.CheckInstalled}))
		r.Use(httprate.Limit(cfg.RateLimitPerMinute, time.Minute, httprate.WithKeyFuncs(appproxy.RateLimitKey)))
		r.Method(http.MethodGet, "/ping", handlers.ProxyPing{})
	})

	// Optional server-to-server API, only mounted when keys exist.
	if len(cfg.Keys) > 0 {
		router.Route("/api/service", func(r chi.Router) {
			r.Use(apikey.Verify(cfg.Keys, logger))
			r.Method(http.MethodGet, "/products", handlers.ListProducts{Admin: adminClient, Shop: shopFromQuery, Logger: logger})
		})
	}

	// Everything else is the embedded admin UI.
	router.NotFound(spa.ServeHTTP)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("[Server] Listening", "port", cfg.Port, "host", cfg.Host)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("[Server] Shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return riverClient.Stop(shutdownCtx)
}

// shopFromSession reads the shop from the verified session token.
func shopFromSession(r *http.Request) (string, error) {
	session, ok := sessiontoken.SessionFrom(r.Context())
	if !ok {
		return "", errors.New("no session")
	}
	return session.Shop, nil
}

// shopFromQuery reads the shop a server-to-server caller asks about.
func shopFromQuery(r *http.Request) (string, error) {
	shop := r.URL.Query().Get("shop")
	if shop == "" {
		return "", errors.New("missing ?shop=<shop>.myshopify.com")
	}
	return shop, nil
}
