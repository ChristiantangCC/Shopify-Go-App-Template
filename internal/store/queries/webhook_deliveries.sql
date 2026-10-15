-- name: ClaimWebhookDelivery :execrows
INSERT INTO webhook_deliveries (webhook_id, shop_domain, topic)
VALUES ($1, $2, $3)
ON CONFLICT (webhook_id) DO NOTHING;

-- name: PruneWebhookDeliveries :execrows
DELETE FROM webhook_deliveries WHERE received_at < $1;
