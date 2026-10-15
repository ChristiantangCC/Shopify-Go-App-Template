// Package tokens owns the lifecycle of a shop's expiring offline token:
// install via token exchange, proactive refresh before expiry, a reactive
// refresh after a 401, and the terminal needs-reauth state that sends the
// merchant back through a token exchange.
package tokens

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/exchange"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
)

// ErrNeedsReauth means no usable token exists and only a new token exchange
// can fix it. Embedded API routes answer it with 401; the frontend then
// re-runs the session bootstrap with a fresh id_token.
var ErrNeedsReauth = errors.New("tokens: shop needs a new token exchange")

// refreshMargin is how close to expiry a token may get before it is renewed.
const refreshMargin = 5 * time.Minute

// Service hands out fresh access tokens.
type Service struct {
	store    *store.Store
	exchange *exchange.Client
	logger   *slog.Logger
}

// New wires the service.
func New(st *store.Store, ex *exchange.Client, logger *slog.Logger) *Service {
	return &Service{store: st, exchange: ex, logger: logger}
}

// EnsureInstall makes sure the shop has a usable offline token, running a
// token exchange only when needed: on first install, after an uninstall, and
// after a terminal refresh failure. A healthy install is left alone, because
// every exchange rotates the stored pair for nothing. It reports whether
// this was a new install.
func (s *Service) EnsureInstall(ctx context.Context, shop, sessionToken string) (bool, error) {
	install, err := s.store.GetInstall(ctx, shop)
	switch {
	case errors.Is(err, store.ErrShopNotFound):
		// fall through to the exchange
	case err != nil:
		return false, err
	case !install.NeedsReauth && !install.Uninstalled && install.AccessToken != "":
		return false, nil
	}

	token, err := s.exchange.Exchange(ctx, shop, sessionToken)
	if err != nil {
		return false, err
	}
	_, isNew, err := s.store.SaveInstall(ctx, shop, token)
	if err != nil {
		return false, err
	}
	s.logger.Info("[Tokens] Install saved", "shop", shop, "new", isNew)
	return isNew, nil
}

// AccessToken returns a token with at least refreshMargin of life left,
// refreshing it first when necessary. Concurrent callers refresh once: they
// serialise on the shop's advisory lock and the losers reuse the winner's
// result.
func (s *Service) AccessToken(ctx context.Context, shop string) (string, error) {
	install, err := s.store.GetInstall(ctx, shop)
	if err != nil {
		if errors.Is(err, store.ErrShopNotFound) {
			return "", ErrNeedsReauth
		}
		return "", err
	}
	if usable, token := usableNow(install); usable {
		return token, nil
	}
	if install.NeedsReauth || install.Uninstalled || install.AccessToken == "" {
		return "", ErrNeedsReauth
	}
	return s.refreshLocked(ctx, shop, "")
}

// ForceRefresh reacts to an Admin API 401: it refreshes unless another
// caller already rotated the failed token away.
func (s *Service) ForceRefresh(ctx context.Context, shop, failedToken string) (string, error) {
	return s.refreshLocked(ctx, shop, failedToken)
}

// Refresh renews the shop's token unconditionally; the keep-alive job uses
// it to rotate refresh tokens that approach their 90-day deadline.
func (s *Service) Refresh(ctx context.Context, shop string) error {
	_, err := s.refreshLocked(ctx, shop, "")
	return err
}

// refreshLocked refreshes under the shop's advisory lock. A non-empty
// staleToken means "refresh even if the expiry looks fine, unless the stored
// token is already a different one".
//
// terminal carries ErrNeedsReauth out of the locked function without making
// it fail: the needs_reauth write must commit, and WithShopLock rolls the
// transaction back when the function returns an error.
func (s *Service) refreshLocked(ctx context.Context, shop, staleToken string) (string, error) {
	var result string
	var terminal error
	err := s.store.WithShopLock(ctx, shop, func(ctx context.Context, locked store.LockedShop) error {
		install, err := locked.Get(ctx)
		if err != nil {
			if errors.Is(err, store.ErrShopNotFound) {
				return ErrNeedsReauth
			}
			return err
		}
		if install.NeedsReauth || install.Uninstalled {
			return ErrNeedsReauth
		}

		// Someone else may have refreshed while this caller waited on the
		// lock; the fresh row settles it.
		if staleToken != "" && install.AccessToken != "" && install.AccessToken != staleToken {
			result = install.AccessToken
			return nil
		}
		if staleToken == "" {
			if usable, token := usableNow(install); usable {
				result = token
				return nil
			}
		}

		if install.RefreshToken == "" {
			// A legacy non-expiring token has nothing to refresh with.
			// Flag the shop so the next app open migrates it through a
			// token exchange.
			terminal = ErrNeedsReauth
			return locked.SetNeedsReauth(ctx)
		}
		if !install.RefreshTokenExpiresAt.IsZero() && time.Now().After(install.RefreshTokenExpiresAt) {
			terminal = ErrNeedsReauth
			return locked.SetNeedsReauth(ctx)
		}

		token, err := s.exchange.Refresh(ctx, shop, install.RefreshToken)
		if errors.Is(err, exchange.ErrInvalidRefreshToken) {
			s.logger.Warn("[Tokens] Refresh token rejected, shop needs reauth", "shop", shop)
			terminal = ErrNeedsReauth
			return locked.SetNeedsReauth(ctx)
		}
		if err != nil {
			// Transient failure. The old token may still have life left;
			// let callers keep using it instead of failing the request.
			if time.Now().Before(install.AccessTokenExpiresAt) {
				s.logger.Warn("[Tokens] Refresh failed, reusing the current token", "shop", shop, "error", err)
				result = install.AccessToken
				return nil
			}
			return fmt.Errorf("tokens: %w", err)
		}

		if err := locked.SaveTokens(ctx, token); err != nil {
			return err
		}
		s.logger.Info("[Tokens] Refreshed", "shop", shop, "expiresAt", token.ExpiresAt)
		result = token.AccessToken
		return nil
	})
	if err != nil {
		return "", err
	}
	if terminal != nil {
		return "", terminal
	}
	return result, nil
}

// usableNow reports whether the install's access token can be used as is.
// A token without an expiry is a legacy non-expiring one and always usable.
func usableNow(install store.Install) (bool, string) {
	if install.NeedsReauth || install.Uninstalled || install.AccessToken == "" {
		return false, ""
	}
	if install.AccessTokenExpiresAt.IsZero() {
		return true, install.AccessToken
	}
	if time.Until(install.AccessTokenExpiresAt) > refreshMargin {
		return true, install.AccessToken
	}
	return false, ""
}
