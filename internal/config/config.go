// Package config loads and validates the app's environment configuration.
// Every variable is documented in .env.example; required ones fail fast at
// boot so a misconfigured deployment never half-works.
package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/apikey"
)

// Config holds everything the app reads from the environment.
type Config struct {
	// Port is the HTTP port the server listens on.
	Port int `env:"PORT" envDefault:"8080"`

	// Host is the app's public base URL (https://...), with no trailing
	// slash. Shopify loads the embedded UI from it and delivers webhooks
	// to it. In development this is the tunnel URL written by
	// scripts/app-dev.sh.
	Host string `env:"HOST,required"`

	// ClientID is the Shopify app's client ID from the Dev Dashboard.
	ClientID string `env:"SHOPIFY_CLIENT_ID,required"`

	// ClientSecret signs session tokens, webhooks and App Proxy requests,
	// and authenticates token exchange.
	ClientSecret string `env:"SHOPIFY_CLIENT_SECRET,required"`

	// APIVersion is the Admin GraphQL API version, like "2026-10".
	APIVersion string `env:"SHOPIFY_API_VERSION" envDefault:"2026-10"`

	// DatabaseURL is the Postgres connection string.
	DatabaseURL string `env:"DATABASE_URL,required"`

	// RedisURL is the Redis connection string (redis:// or rediss://).
	// Optional: Redis only caches (installs, settings) and shares the App
	// Proxy rate limit across instances; Postgres stays the source of
	// truth. Empty means no caching and a per-instance rate limit.
	RedisURL string `env:"REDIS_URL"`

	// EncryptionKey encrypts access and refresh tokens at rest:
	// 64 hex characters (32 bytes). Generate with: openssl rand -hex 32
	EncryptionKey string `env:"ENCRYPTION_KEY,required"`

	// EncryptionKeysOld are earlier ENCRYPTION_KEY values, comma separated,
	// kept only while the re-encrypt job moves tokens to the current key.
	// See docs/key-rotation.md.
	EncryptionKeysOld []string `env:"ENCRYPTION_KEYS_OLD" envSeparator:","`

	// BillingEnabled switches the optional billing module on.
	BillingEnabled bool `env:"BILLING_ENABLED" envDefault:"false"`

	// AppHandle is the app's handle from the Dev Dashboard, used to build
	// the managed-pricing plans URL when billing is enabled.
	AppHandle string `env:"SHOPIFY_APP_HANDLE"`

	// APIKey, when set, becomes a key named "default" for the optional
	// server-to-server /api/service routes.
	APIKey string `env:"API_KEY"`

	// APIKeys holds additional named keys as comma-separated name:key pairs.
	APIKeys []string `env:"API_KEYS" envSeparator:","`

	// Keys is derived from APIKey and APIKeys; it is not read from the
	// environment itself.
	Keys []apikey.Key `env:"-"`

	// ProxyMaxAge is how old an App Proxy request's timestamp may be.
	ProxyMaxAge time.Duration `env:"PROXY_MAX_AGE" envDefault:"5m"`

	// RateLimitPerMinute caps App Proxy requests per customer or IP.
	RateLimitPerMinute int `env:"RATE_LIMIT_PER_MINUTE" envDefault:"30"`

	// ProxyTrustedHops is how many proxies in front of the app append to
	// X-Forwarded-For: 2 for Shopify's App Proxy plus one reverse proxy of
	// your own, 1 with none. The rate limiter reads the visitor's IP that
	// many entries from the right, so a spoofed header can't dodge it.
	ProxyTrustedHops int `env:"PROXY_TRUSTED_HOPS" envDefault:"2"`

	// FrontendDevURL, when set, makes the server proxy non-API requests to
	// a running Vite dev server for hot reload. Leave empty in production,
	// where the embedded build is served instead.
	FrontendDevURL string `env:"FRONTEND_DEV_URL"`

	// ShutdownDrainDelay is how long the server keeps serving after SIGTERM
	// with /readyz already failing, so a load balancer can take the
	// instance out of rotation before connections close. Set it a little
	// above the readiness probe's period (e.g. 10s on Kubernetes); 0 shuts
	// down at once.
	ShutdownDrainDelay time.Duration `env:"SHUTDOWN_DRAIN_DELAY" envDefault:"0s"`

	// LogLevel is debug, info, warn or error.
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`

	// LogFormat is json or text.
	LogFormat string `env:"LOG_FORMAT" envDefault:"json"`
}

// Load reads the environment into a Config and validates it.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	cfg.Host = strings.TrimRight(strings.TrimSpace(cfg.Host), "/")
	host, err := url.Parse(cfg.Host)
	if err != nil || host.Scheme != "https" || host.Host == "" {
		return Config{}, fmt.Errorf("config: HOST must be an https URL, got %q", cfg.Host)
	}

	// A trailing comma or blank entry in ENCRYPTION_KEYS_OLD is not a key.
	var oldKeys []string
	for _, key := range cfg.EncryptionKeysOld {
		if key = strings.TrimSpace(key); key != "" {
			oldKeys = append(oldKeys, key)
		}
	}
	cfg.EncryptionKeysOld = oldKeys

	if cfg.ProxyTrustedHops < 1 {
		return Config{}, fmt.Errorf("config: PROXY_TRUSTED_HOPS must be at least 1, got %d", cfg.ProxyTrustedHops)
	}

	keys, err := apikey.Parse(cfg.APIKey, cfg.APIKeys)
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	cfg.Keys = keys

	return cfg, nil
}
