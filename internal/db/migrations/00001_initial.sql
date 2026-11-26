-- The schema AutoMigrate created before migrations were versioned. Every
-- statement is IF NOT EXISTS, so a database that AutoMigrate already built
-- adopts this migration without changes.

-- +goose Up
CREATE TABLE IF NOT EXISTS shops (
    id bigserial CONSTRAINT shops_pkey PRIMARY KEY,
    shop_domain text NOT NULL,
    access_token_enc text,
    refresh_token_enc text,
    scopes text NOT NULL,
    access_token_expires_at timestamptz,
    refresh_token_expires_at timestamptz,
    needs_reauth boolean NOT NULL,
    installed_at timestamptz NOT NULL,
    uninstalled_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_shops_shop_domain ON shops (shop_domain);

-- Settings cascade from shops, so shop/redact's single delete covers them.
CREATE TABLE IF NOT EXISTS shop_settings (
    shop_domain text CONSTRAINT shop_settings_pkey PRIMARY KEY
        CONSTRAINT fk_shops_settings REFERENCES shops (shop_domain) ON DELETE CASCADE,
    notification_email text NOT NULL,
    sync_enabled boolean NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    webhook_id text CONSTRAINT webhook_deliveries_pkey PRIMARY KEY,
    shop_domain text NOT NULL,
    topic text NOT NULL,
    received_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_received_at ON webhook_deliveries (received_at);

-- +goose Down
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS shop_settings;
DROP TABLE IF EXISTS shops;
