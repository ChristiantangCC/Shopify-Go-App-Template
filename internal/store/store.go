// Package store persists shop installs, settings and webhook deliveries
// through GORM. It seals tokens before they reach the database (tokens never
// touch it in plain text), serialises token refreshes with a per-shop
// advisory lock, and caches shop settings in Redis.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/cache"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/crypto"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/exchange"
)

// settingsTTL is how long a shop's settings stay cached. Saving settings
// invalidates the entry at once; the TTL only bounds staleness if that
// invalidation fails.
const settingsTTL = 5 * time.Minute

// ErrShopNotFound means no install row exists for the shop.
var ErrShopNotFound = errors.New("store: shop not found")

// Install is a shop row with its tokens decrypted. Zero times mean "not set"
// (a legacy non-expiring token has no expiry).
type Install struct {
	Shop                  string
	AccessToken           string
	RefreshToken          string
	Scopes                string
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
	NeedsReauth           bool
	Uninstalled           bool
	InstalledAt           time.Time
}

// Store reads and writes shop installs.
type Store struct {
	db     *gorm.DB
	cipher *crypto.Cipher
	cache  *cache.Cache
}

// New wraps a GORM handle, the token cipher and the cache. A nil cache
// disables caching.
func New(db *gorm.DB, cipher *crypto.Cipher, c *cache.Cache) *Store {
	return &Store{db: db, cipher: cipher, cache: c}
}

// byShop is the WHERE clause every per-shop query uses.
func byShop(shop string) *Shop { return &Shop{ShopDomain: shop} }

// GetInstall returns the shop's install with decrypted tokens.
func (s *Store) GetInstall(ctx context.Context, shop string) (Install, error) {
	return s.getInstall(s.db.WithContext(ctx), shop)
}

func (s *Store) getInstall(db *gorm.DB, shop string) (Install, error) {
	var row Shop
	err := db.Where(byShop(shop)).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Install{}, ErrShopNotFound
	}
	if err != nil {
		return Install{}, fmt.Errorf("store: %w", err)
	}
	return s.install(row)
}

// SaveInstall stores a token-exchange result, reviving the row if the shop
// had uninstalled. It reports whether this is a new install (no row yet, or
// the previous one was uninstalled). It holds the shop's lock, so it never
// interleaves with a token refresh.
func (s *Store) SaveInstall(ctx context.Context, shop string, token exchange.Token) (Install, bool, error) {
	accessEnc, refreshEnc, err := s.seal(token)
	if err != nil {
		return Install{}, false, err
	}

	var row Shop
	var isNew bool
	err = s.withShopLock(ctx, shop, func(tx *gorm.DB) error {
		err := tx.Where(byShop(shop)).Take(&row).Error
		now := time.Now()
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			isNew = true
			row = Shop{ShopDomain: shop, InstalledAt: now}
		case err != nil:
			return err
		case row.UninstalledAt != nil:
			isNew = true
			row.InstalledAt = now
		}
		row.AccessTokenEnc = accessEnc
		row.RefreshTokenEnc = refreshEnc
		row.Scopes = token.Scope
		row.AccessTokenExpiresAt = timePtr(token.ExpiresAt)
		row.RefreshTokenExpiresAt = timePtr(token.RefreshTokenExpiresAt)
		row.NeedsReauth = false
		row.UninstalledAt = nil
		// Save inserts a new row (zero ID) or writes every column of an
		// existing one, nil tokens included.
		return tx.Save(&row).Error
	})
	if err != nil {
		return Install{}, false, fmt.Errorf("store: %w", err)
	}
	install, err := s.install(row)
	return install, isNew, err
}

// SetNeedsReauth flags the shop as needing a fresh token exchange.
func (s *Store) SetNeedsReauth(ctx context.Context, shop string) error {
	return setNeedsReauth(s.db.WithContext(ctx), shop)
}

func setNeedsReauth(db *gorm.DB, shop string) error {
	if err := db.Model(&Shop{}).Where(byShop(shop)).Update("NeedsReauth", true).Error; err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// SetScopes records the scope set Shopify reports for the shop.
func (s *Store) SetScopes(ctx context.Context, shop, scopes string) error {
	if err := s.db.WithContext(ctx).Model(&Shop{}).Where(byShop(shop)).Update("Scopes", scopes).Error; err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// MarkUninstalled wipes the shop's tokens and records the uninstall. The row
// itself stays until shop/redact arrives, as Shopify's data rules expect.
func (s *Store) MarkUninstalled(ctx context.Context, shop string) error {
	now := time.Now()
	err := s.db.WithContext(ctx).Model(&Shop{}).Where(byShop(shop)).
		// Select names the columns to write, so the nil tokens are written
		// too instead of being skipped as zero values.
		Select("AccessTokenEnc", "RefreshTokenEnc", "AccessTokenExpiresAt", "RefreshTokenExpiresAt", "UninstalledAt").
		Updates(&Shop{UninstalledAt: &now}).Error
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// Delete removes the shop row entirely (shop/redact). Its settings cascade
// away in the database, and their cache entry goes with them.
func (s *Store) Delete(ctx context.Context, shop string) error {
	if err := s.db.WithContext(ctx).Where(byShop(shop)).Delete(&Shop{}).Error; err != nil {
		return fmt.Errorf("store: %w", err)
	}
	s.cache.Delete(ctx, settingsKey(shop))
	return nil
}

// CheckInstalled returns nil only when the shop has a live install. It is
// the App Proxy's CheckShop.
func (s *Store) CheckInstalled(ctx context.Context, shop string) error {
	install, err := s.GetInstall(ctx, shop)
	if err != nil {
		return err
	}
	if install.Uninstalled {
		return errors.New("store: shop uninstalled")
	}
	return nil
}

// ListShopsWithExpiringRefreshTokens returns live installs whose refresh
// token expires before the deadline, for the keep-alive job.
func (s *Store) ListShopsWithExpiringRefreshTokens(ctx context.Context, before time.Time) ([]string, error) {
	var shops []string
	err := s.db.WithContext(ctx).Model(&Shop{}).
		Where(map[string]any{"needs_reauth": false}).
		Clauses(clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "uninstalled_at"}, Value: nil},
			clause.Neq{Column: clause.Column{Name: "refresh_token_enc"}, Value: nil},
			clause.Lt{Column: clause.Column{Name: "refresh_token_expires_at"}, Value: before},
		}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "refresh_token_expires_at"}}).
		Pluck("shop_domain", &shops).Error
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	return shops, nil
}

// Settings are the per-shop settings the Settings page edits. A shop
// without a row gets the defaults.
type Settings struct {
	NotificationEmail string `json:"notificationEmail"`
	SyncEnabled       bool   `json:"syncEnabled"`
}

// defaultSettings is what a shop that never saved settings gets.
func defaultSettings() Settings { return Settings{SyncEnabled: true} }

func settingsKey(shop string) string { return "shop-settings:" + shop }

// GetSettings returns the shop's settings, or the defaults when none were
// saved yet. Reads are cached in Redis for settingsTTL.
func (s *Store) GetSettings(ctx context.Context, shop string) (Settings, error) {
	return cache.GetOrLoad(ctx, s.cache, settingsKey(shop), settingsTTL, func(ctx context.Context) (Settings, error) {
		var row ShopSettings
		err := s.db.WithContext(ctx).Where(&ShopSettings{ShopDomain: shop}).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return defaultSettings(), nil
		}
		if err != nil {
			return Settings{}, fmt.Errorf("store: %w", err)
		}
		return Settings{NotificationEmail: row.NotificationEmail, SyncEnabled: row.SyncEnabled}, nil
	})
}

// SaveSettings upserts the shop's settings and invalidates the cached copy.
func (s *Store) SaveSettings(ctx context.Context, shop string, settings Settings) (Settings, error) {
	row := ShopSettings{
		ShopDomain:        shop,
		NotificationEmail: settings.NotificationEmail,
		SyncEnabled:       settings.SyncEnabled,
	}
	// UpdateAll rewrites every column except the key and CreatedAt.
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
		return Settings{}, fmt.Errorf("store: %w", err)
	}
	s.cache.Delete(ctx, settingsKey(shop))
	return Settings{NotificationEmail: row.NotificationEmail, SyncEnabled: row.SyncEnabled}, nil
}

// Transaction runs fn in a database transaction: commit when fn returns nil,
// roll back otherwise.
func (s *Store) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return s.db.WithContext(ctx).Transaction(fn)
}

// SQLTx unwraps the database/sql transaction under a GORM transaction, for
// libraries that join it (River's InsertTx).
func SQLTx(tx *gorm.DB) (*sql.Tx, error) {
	sqlTx, ok := tx.Statement.ConnPool.(*sql.Tx)
	if !ok {
		return nil, errors.New("store: not inside a transaction")
	}
	return sqlTx, nil
}

// ClaimWebhookDelivery records a delivery id inside tx. It reports false
// when the id was already claimed, meaning the delivery is a duplicate.
func ClaimWebhookDelivery(tx *gorm.DB, delivery WebhookDelivery) (bool, error) {
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&delivery)
	if result.Error != nil {
		return false, fmt.Errorf("store: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// PruneWebhookDeliveries deletes dedupe rows received before the cutoff and
// returns how many went.
func (s *Store) PruneWebhookDeliveries(ctx context.Context, before time.Time) (int64, error) {
	result := s.db.WithContext(ctx).
		Clauses(clause.Where{Exprs: []clause.Expression{
			clause.Lt{Column: clause.Column{Name: "received_at"}, Value: before},
		}}).
		Delete(&WebhookDelivery{})
	if result.Error != nil {
		return 0, fmt.Errorf("store: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// LockedShop works on one shop's row inside a transaction that holds the
// shop's advisory lock.
type LockedShop struct {
	store *Store
	tx    *gorm.DB
	shop  string
}

// Get re-reads the row under the lock.
func (l LockedShop) Get(ctx context.Context) (Install, error) {
	return l.store.getInstall(l.tx.WithContext(ctx), l.shop)
}

// SaveTokens persists a refreshed token pair.
func (l LockedShop) SaveTokens(ctx context.Context, token exchange.Token) error {
	accessEnc, refreshEnc, err := l.store.seal(token)
	if err != nil {
		return err
	}
	result := l.tx.WithContext(ctx).Model(&Shop{}).Where(byShop(l.shop)).
		Select("AccessTokenEnc", "RefreshTokenEnc", "AccessTokenExpiresAt", "RefreshTokenExpiresAt", "NeedsReauth").
		Updates(&Shop{
			AccessTokenEnc:        accessEnc,
			RefreshTokenEnc:       refreshEnc,
			AccessTokenExpiresAt:  timePtr(token.ExpiresAt),
			RefreshTokenExpiresAt: timePtr(token.RefreshTokenExpiresAt),
			NeedsReauth:           false,
		})
	if result.Error != nil {
		return fmt.Errorf("store: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrShopNotFound
	}
	return nil
}

// SetNeedsReauth flags the shop inside the locked transaction.
func (l LockedShop) SetNeedsReauth(ctx context.Context) error {
	return setNeedsReauth(l.tx.WithContext(ctx), l.shop)
}

// WithShopLock runs fn while holding the shop's transaction-scoped advisory
// lock. Concurrent callers for the same shop, across every instance of the
// app, run fn one at a time; each gets a fresh view of the row.
func (s *Store) WithShopLock(ctx context.Context, shop string, fn func(ctx context.Context, locked LockedShop) error) error {
	return s.withShopLock(ctx, shop, func(tx *gorm.DB) error {
		return fn(ctx, LockedShop{store: s, tx: tx, shop: shop})
	})
}

func (s *Store) withShopLock(ctx context.Context, shop string, fn func(tx *gorm.DB) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Advisory locks have no ORM equivalent; this is the one raw
		// statement the store runs.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "shop-token:"+shop).Error; err != nil {
			return fmt.Errorf("store: lock: %w", err)
		}
		return fn(tx)
	})
}

func (s *Store) install(row Shop) (Install, error) {
	install := Install{
		Shop:        row.ShopDomain,
		Scopes:      row.Scopes,
		NeedsReauth: row.NeedsReauth,
		Uninstalled: row.UninstalledAt != nil,
		InstalledAt: row.InstalledAt,
	}
	if row.AccessTokenExpiresAt != nil {
		install.AccessTokenExpiresAt = *row.AccessTokenExpiresAt
	}
	if row.RefreshTokenExpiresAt != nil {
		install.RefreshTokenExpiresAt = *row.RefreshTokenExpiresAt
	}
	var err error
	if install.AccessToken, err = s.open(row.AccessTokenEnc); err != nil {
		return Install{}, err
	}
	if install.RefreshToken, err = s.open(row.RefreshTokenEnc); err != nil {
		return Install{}, err
	}
	return install, nil
}

func (s *Store) open(sealed *string) (string, error) {
	if sealed == nil || *sealed == "" {
		return "", nil
	}
	return s.cipher.Open(*sealed)
}

// seal encrypts a token pair. A missing refresh token (legacy non-expiring
// tokens) seals to nil, which clears the column.
func (s *Store) seal(token exchange.Token) (access, refresh *string, err error) {
	accessEnc, err := s.cipher.Seal(token.AccessToken)
	if err != nil {
		return nil, nil, err
	}
	if token.RefreshToken == "" {
		return &accessEnc, nil, nil
	}
	refreshEnc, err := s.cipher.Seal(token.RefreshToken)
	if err != nil {
		return nil, nil, err
	}
	return &accessEnc, &refreshEnc, nil
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
