// Package store persists shop installs. It wraps the sqlc-generated queries
// with token encryption (tokens never touch the database in plain text) and
// a per-shop advisory lock used to serialise token refreshes.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/crypto"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/exchange"
	storegen "github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store/gen"
)

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
	pool   *pgxpool.Pool
	q      *storegen.Queries
	cipher *crypto.Cipher
}

// New wraps a pool and a cipher.
func New(pool *pgxpool.Pool, cipher *crypto.Cipher) *Store {
	return &Store{pool: pool, q: storegen.New(pool), cipher: cipher}
}

// Pool exposes the underlying pool for callers that need a transaction, like
// the webhook intake which claims a delivery and enqueues its job atomically.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// GetInstall returns the shop's install with decrypted tokens.
func (s *Store) GetInstall(ctx context.Context, shop string) (Install, error) {
	row, err := s.q.GetShop(ctx, shop)
	if errors.Is(err, pgx.ErrNoRows) {
		return Install{}, ErrShopNotFound
	}
	if err != nil {
		return Install{}, fmt.Errorf("store: %w", err)
	}
	return s.install(row)
}

// SaveInstall stores a token-exchange result, reviving the row if the shop
// had uninstalled. It reports whether this is a new install (no row yet, or
// the previous one was uninstalled).
func (s *Store) SaveInstall(ctx context.Context, shop string, token exchange.Token) (Install, bool, error) {
	existing, err := s.q.GetShop(ctx, shop)
	isNew := errors.Is(err, pgx.ErrNoRows) || (err == nil && existing.UninstalledAt != nil)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Install{}, false, fmt.Errorf("store: %w", err)
	}

	params, err := s.upsertParams(shop, token)
	if err != nil {
		return Install{}, false, err
	}
	row, err := s.q.UpsertShopInstall(ctx, params)
	if err != nil {
		return Install{}, false, fmt.Errorf("store: %w", err)
	}
	install, err := s.install(row)
	return install, isNew, err
}

// SetNeedsReauth flags the shop as needing a fresh token exchange.
func (s *Store) SetNeedsReauth(ctx context.Context, shop string) error {
	return s.q.SetShopNeedsReauth(ctx, shop)
}

// SetScopes records the scope set Shopify reports for the shop.
func (s *Store) SetScopes(ctx context.Context, shop, scopes string) error {
	return s.q.SetShopScopes(ctx, storegen.SetShopScopesParams{ShopDomain: shop, Scopes: scopes})
}

// MarkUninstalled wipes the shop's tokens and records the uninstall. The row
// itself stays until shop/redact arrives, as Shopify's data rules expect.
func (s *Store) MarkUninstalled(ctx context.Context, shop string) error {
	return s.q.MarkShopUninstalled(ctx, shop)
}

// Delete removes the shop row entirely (shop/redact).
func (s *Store) Delete(ctx context.Context, shop string) error {
	return s.q.DeleteShop(ctx, shop)
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

// ListShopsWithExpiringRefreshTokens returns installs whose refresh token
// expires before the deadline, for the keep-alive job.
func (s *Store) ListShopsWithExpiringRefreshTokens(ctx context.Context, before time.Time) ([]string, error) {
	rows, err := s.q.ListShopsWithExpiringRefreshTokens(ctx, &before)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	shops := make([]string, len(rows))
	for i, row := range rows {
		shops[i] = row.ShopDomain
	}
	return shops, nil
}

// LockedShop works on one shop's row inside a transaction that holds the
// shop's advisory lock.
type LockedShop struct {
	store *Store
	q     *storegen.Queries
	shop  string
}

// Get re-reads the row under the lock.
func (l LockedShop) Get(ctx context.Context) (Install, error) {
	row, err := l.q.GetShop(ctx, l.shop)
	if errors.Is(err, pgx.ErrNoRows) {
		return Install{}, ErrShopNotFound
	}
	if err != nil {
		return Install{}, fmt.Errorf("store: %w", err)
	}
	return l.store.install(row)
}

// SaveTokens persists a refreshed token pair.
func (l LockedShop) SaveTokens(ctx context.Context, token exchange.Token) error {
	accessEnc, err := l.store.cipher.Seal(token.AccessToken)
	if err != nil {
		return err
	}
	params := storegen.UpdateShopTokensParams{
		ShopDomain:            l.shop,
		AccessTokenEnc:        &accessEnc,
		AccessTokenExpiresAt:  timePtr(token.ExpiresAt),
		RefreshTokenExpiresAt: timePtr(token.RefreshTokenExpiresAt),
	}
	if token.RefreshToken != "" {
		refreshEnc, err := l.store.cipher.Seal(token.RefreshToken)
		if err != nil {
			return err
		}
		params.RefreshTokenEnc = &refreshEnc
	}
	if _, err := l.q.UpdateShopTokens(ctx, params); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// SetNeedsReauth flags the shop inside the locked transaction.
func (l LockedShop) SetNeedsReauth(ctx context.Context) error {
	return l.q.SetShopNeedsReauth(ctx, l.shop)
}

// WithShopLock runs fn while holding the shop's transaction-scoped advisory
// lock. Concurrent callers for the same shop, across every instance of the
// app, run fn one at a time; each gets a fresh view of the row.
func (s *Store) WithShopLock(ctx context.Context, shop string, fn func(ctx context.Context, locked LockedShop) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('shop-token:' || $1::text))", shop); err != nil {
		return fmt.Errorf("store: lock: %w", err)
	}
	if err := fn(ctx, LockedShop{store: s, q: storegen.New(tx), shop: shop}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

func (s *Store) install(row storegen.Shop) (Install, error) {
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

func (s *Store) upsertParams(shop string, token exchange.Token) (storegen.UpsertShopInstallParams, error) {
	accessEnc, err := s.cipher.Seal(token.AccessToken)
	if err != nil {
		return storegen.UpsertShopInstallParams{}, err
	}
	params := storegen.UpsertShopInstallParams{
		ShopDomain:            shop,
		AccessTokenEnc:        &accessEnc,
		Scopes:                token.Scope,
		AccessTokenExpiresAt:  timePtr(token.ExpiresAt),
		RefreshTokenExpiresAt: timePtr(token.RefreshTokenExpiresAt),
	}
	if token.RefreshToken != "" {
		refreshEnc, err := s.cipher.Seal(token.RefreshToken)
		if err != nil {
			return storegen.UpsertShopInstallParams{}, err
		}
		params.RefreshTokenEnc = &refreshEnc
	}
	return params, nil
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
