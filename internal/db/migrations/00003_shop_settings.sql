-- +goose Up
-- Per-shop settings, the canonical "store something per shop" pattern.
-- Cascading from shops means shop/redact wipes these rows automatically.
CREATE TABLE shop_settings (
    shop_domain TEXT PRIMARY KEY REFERENCES shops (shop_domain) ON DELETE CASCADE,
    notification_email TEXT NOT NULL DEFAULT '',
    sync_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE shop_settings;
