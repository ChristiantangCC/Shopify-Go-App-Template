-- name: GetShopSettings :one
SELECT * FROM shop_settings WHERE shop_domain = $1;

-- name: UpsertShopSettings :one
INSERT INTO shop_settings (shop_domain, notification_email, sync_enabled)
VALUES ($1, $2, $3)
ON CONFLICT (shop_domain) DO UPDATE SET
    notification_email = EXCLUDED.notification_email,
    sync_enabled = EXCLUDED.sync_enabled,
    updated_at = now()
RETURNING *;
