-- name: GetShop :one
SELECT * FROM shops WHERE shop_domain = $1;

-- name: UpsertShopInstall :one
INSERT INTO shops (shop_domain, access_token_enc, refresh_token_enc, scopes, access_token_expires_at, refresh_token_expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (shop_domain) DO UPDATE SET
    access_token_enc = EXCLUDED.access_token_enc,
    refresh_token_enc = EXCLUDED.refresh_token_enc,
    scopes = EXCLUDED.scopes,
    access_token_expires_at = EXCLUDED.access_token_expires_at,
    refresh_token_expires_at = EXCLUDED.refresh_token_expires_at,
    needs_reauth = FALSE,
    uninstalled_at = NULL,
    installed_at = CASE WHEN shops.uninstalled_at IS NOT NULL THEN now() ELSE shops.installed_at END,
    updated_at = now()
RETURNING *;

-- name: UpdateShopTokens :one
UPDATE shops SET
    access_token_enc = $2,
    refresh_token_enc = $3,
    access_token_expires_at = $4,
    refresh_token_expires_at = $5,
    needs_reauth = FALSE,
    updated_at = now()
WHERE shop_domain = $1
RETURNING *;

-- name: SetShopNeedsReauth :exec
UPDATE shops SET needs_reauth = TRUE, updated_at = now() WHERE shop_domain = $1;

-- name: SetShopScopes :exec
UPDATE shops SET scopes = $2, updated_at = now() WHERE shop_domain = $1;

-- name: MarkShopUninstalled :exec
UPDATE shops SET
    access_token_enc = NULL,
    refresh_token_enc = NULL,
    access_token_expires_at = NULL,
    refresh_token_expires_at = NULL,
    uninstalled_at = now(),
    updated_at = now()
WHERE shop_domain = $1;

-- name: DeleteShop :exec
DELETE FROM shops WHERE shop_domain = $1;

-- name: ListShopsWithExpiringRefreshTokens :many
SELECT * FROM shops
WHERE uninstalled_at IS NULL
  AND needs_reauth = FALSE
  AND refresh_token_enc IS NOT NULL
  AND refresh_token_expires_at < $1
ORDER BY refresh_token_expires_at;
