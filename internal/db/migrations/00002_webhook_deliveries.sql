-- +goose Up
-- One row per processed webhook delivery, keyed on X-Shopify-Webhook-Id.
-- Claiming a delivery and enqueuing its job happen in one transaction, so a
-- delivery is either fully handed to the queue or not claimed at all.
CREATE TABLE webhook_deliveries (
    webhook_id TEXT PRIMARY KEY,
    shop_domain TEXT NOT NULL,
    topic TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX webhook_deliveries_received_at_idx ON webhook_deliveries (received_at);

-- +goose Down
DROP TABLE webhook_deliveries;
