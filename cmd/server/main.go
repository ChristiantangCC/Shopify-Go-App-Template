// The server binary runs everything: HTTP (embedded admin UI, its API, the
// webhook intake, App Proxy and server-to-server routes) plus the River
// workers that process webhooks and scheduled jobs. One process, backed by
// Postgres (data and jobs) and Redis (cache only).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
	httprateredis "github.com/go-chi/httprate-redis"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/apikey"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/appproxy"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/cache"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/config"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/crypto"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/db"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/handlers"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/jobs"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/logging"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/admin"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/exchange"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/sessiontoken"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/shopdomain"
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

	database, err := db.Connect(ctx, cfg.DatabaseURL, logger)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := db.Migrate(ctx, database, logger); err != nil {
		return err
	}
	logger.Info("[Server] Database ready")

	// Redis is optional: a nil *cache.Cache caches nothing.
	var redisCache *cache.Cache
	if cfg.RedisURL != "" {
		if redisCache, err = cache.Connect(ctx, cfg.RedisURL, logger); err != nil {
			return err
		}
		defer func() { _ = redisCache.Close() }()
		logger.Info("[Server] Redis ready")
	} else {
		logger.Info("[Server] REDIS_URL not set, running without a cache")
	}

	cipher, err := crypto.New(cfg.EncryptionKey, cfg.EncryptionKeysOld...)
	if err != nil {
		return err
	}
	logger.Info("[Server] Encryption ready", "key", cipher.CurrentKeyID(), "oldKeys", len(cfg.EncryptionKeysOld))
	shops := store.New(database.Gorm, cipher, redisCache)
	tokenService := tokens.New(shops, exchange.New(cfg.ClientID, cfg.ClientSecret), logger)
	adminClient := admin.New(cfg.APIVersion, tokenService)
	subscriber := webhooks.NewSubscriber(webhooks.Deps{Store: shops, Admin: adminClient, Logger: logger}, cfg.Host)

	riverClient, err := jobs.NewClient(jobs.Options{
		Pool:       database.Pool,
		Store:      shops,
		Admin:      adminClient,
		Tokens:     tokenService,
		Subscriber: subscriber,
		KeyID:      cipher.CurrentKeyID(),
		Logger:     logger,
	})
	if err != nil {
		return err
	}
	if err := riverClient.Start(ctx); err != nil {
		return fmt.Errorf("jobs: %w", err)
	}
	logger.Info("[Server] Workers running")

	inserter, err := jobs.NewInserter(database.SQL, logger)
	if err != nil {
		return err
	}

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

	probes := &handlers.Probes{Pool: database.Pool, Cache: redisCache}
	router.Get("/livez", probes.Live)
	router.Get("/readyz", probes.Ready)
	router.Get("/health", probes.Ready)
	router.Method(http.MethodPost, "/webhooks", handlers.Webhook{Secret: cfg.ClientSecret, Store: shops, Inserter: inserter, Logger: logger})
	router.Method(http.MethodPost, "/events", handlers.Events{Secret: cfg.ClientSecret, Store: shops, Inserter: inserter, Logger: logger})

	// Embedded admin API: every route behind session-token verification.
	router.Route("/api/admin", func(r chi.Router) {
		r.Use(sessiontoken.Middleware(cfg.ClientID, cfg.ClientSecret, logger))
		r.Method(http.MethodPost, "/session", handlers.Session{Tokens: tokenService, River: riverClient, Host: cfg.Host, BillingEnabled: cfg.BillingEnabled, Logger: logger})
		r.Method(http.MethodGet, "/products", handlers.ListProducts{Admin: adminClient, Shop: shopFromSession, Logger: logger})
		webhookSubs := handlers.WebhookSubscriptions{Subscriber: subscriber, Logger: logger}
		r.Get("/webhooks/subscriptions", webhookSubs.List)
		r.Post("/webhooks/subscriptions", webhookSubs.Toggle)
		settings := handlers.Settings{Store: shops, River: riverClient, Host: cfg.Host, Logger: logger}
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
		r.Use(httprate.LimitBy(cfg.RateLimitPerMinute, time.Minute, appproxy.RateLimitKey(cfg.ProxyTrustedHops), proxyLimitOptions(redisCache, logger)...))
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
		// Fail /readyz first and keep serving for the drain delay, so the
		// load balancer stops routing here before the listener closes.
		probes.Drain()
		if cfg.ShutdownDrainDelay > 0 {
			logger.Info("[Server] Draining", "delay", cfg.ShutdownDrainDelay)
			time.Sleep(cfg.ShutdownDrainDelay)
		}
	}

	// HTTP and the workers drain in parallel, each within the same 10 s,
	// and the workers stop even when the HTTP shutdown fails.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	riverStopped := make(chan error, 1)
	go func() { riverStopped <- riverClient.Stop(shutdownCtx) }()
	httpErr := server.Shutdown(shutdownCtx)
	if httpErr != nil {
		httpErr = fmt.Errorf("http shutdown: %w", httpErr)
	}
	riverErr := <-riverStopped
	if riverErr != nil {
		riverErr = fmt.Errorf("jobs shutdown: %w", riverErr)
	}
	return errors.Join(httpErr, riverErr)
}

// proxyLimitOptions shares the App Proxy rate-limit counters through Redis
// when it is configured, so the limit holds across every instance;
// httprate-redis falls back to a local counter while Redis is unreachable
// instead of failing requests. Without Redis each instance counts alone.
func proxyLimitOptions(redisCache *cache.Cache, logger *slog.Logger) []httprate.Option {
	var options []httprate.Option
	if client := redisCache.Client(); client != nil {
		options = append(options, httprateredis.WithRedisLimitCounter(&httprateredis.Config{
			Client:       client,
			PrefixKey:    "ratelimit:proxy",
			WindowLength: time.Minute,
			OnError: func(err error) {
				logger.Warn("[RateLimit] Redis counter failed, using local fallback", "error", err)
			},
		}))
	}
	return options
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
	if !shopdomain.Valid(shop) {
		return "", errors.New("?shop must be a <shop>.myshopify.com domain")
	}
	return shop, nil
}
