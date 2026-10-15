// Package exchange talks to Shopify's token endpoint: it trades a verified
// session token for an expiring offline Admin API token (token exchange,
// expiring=1) and renews that token with the refresh grant. Both calls hit
// POST https://<shop>/admin/oauth/access_token with form-encoded bodies.
package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrInvalidRefreshToken means Shopify rejected the refresh token for good:
// it expired, was already rotated past, or the shop reinstalled. The only way
// forward is a new token exchange, which needs a merchant to open the app.
var ErrInvalidRefreshToken = errors.New("exchange: refresh token no longer valid")

// Token is one issued access token pair.
type Token struct {
	AccessToken string
	Scope       string
	// ExpiresAt is when the access token stops working (about an hour out).
	ExpiresAt time.Time
	// RefreshToken renews the pair; it rotates on every refresh.
	RefreshToken string
	// RefreshTokenExpiresAt is the refresh token's hard deadline (90 days
	// when issued).
	RefreshTokenExpiresAt time.Time
}

// Client calls the token endpoint. It is safe for concurrent use.
type Client struct {
	clientID     string
	clientSecret string
	// HTTPClient overrides the default 15 s-timeout client; tests point it
	// at a fake token endpoint.
	HTTPClient *http.Client
	now        func() time.Time
}

// New builds a Client for this app's credentials.
func New(clientID, clientSecret string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		now:          time.Now,
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Exchange trades a verified session token for an expiring offline token.
func (c *Client) Exchange(ctx context.Context, shop, sessionToken string) (Token, error) {
	form := url.Values{
		"grant_type":           {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token":        {sessionToken},
		"subject_token_type":   {"urn:ietf:params:oauth:token-type:id_token"},
		"requested_token_type": {"urn:shopify:params:oauth:token-type:offline-access-token"},
		"client_id":            {c.clientID},
		"client_secret":        {c.clientSecret},
		"expiring":             {"1"},
	}
	token, err := c.post(ctx, shop, form)
	if err != nil {
		return Token{}, fmt.Errorf("token exchange: %w", err)
	}
	return token, nil
}

// Refresh renews an expiring offline token. Both tokens rotate: persist the
// returned pair and stop using the presented one.
func (c *Client) Refresh(ctx context.Context, shop, refreshToken string) (Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
	}
	token, err := c.post(ctx, shop, form)
	if err != nil {
		if errors.Is(err, ErrInvalidRefreshToken) {
			return Token{}, err
		}
		return Token{}, fmt.Errorf("token refresh: %w", err)
	}
	return token, nil
}

func (c *Client) post(ctx context.Context, shop string, form url.Values) (Token, error) {
	endpoint := fmt.Sprintf("https://%s/admin/oauth/access_token", shop)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	res, err := c.httpClient().Do(req)
	if err != nil {
		return Token{}, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))

	// Shopify answers 401 {"error":"invalid_request"} for a refresh token
	// that expired, was rotated past, or belongs to a gone install.
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return Token{}, ErrInvalidRefreshToken
	}
	if res.StatusCode != http.StatusOK {
		return Token{}, fmt.Errorf("Shopify answered %d", res.StatusCode)
	}

	var parsed struct {
		AccessToken           string `json:"access_token"`
		Scope                 string `json:"scope"`
		ExpiresIn             int64  `json:"expires_in"`
		RefreshToken          string `json:"refresh_token"`
		RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed.AccessToken == "" {
		return Token{}, errors.New("unexpected response")
	}

	token := Token{AccessToken: parsed.AccessToken, Scope: parsed.Scope, RefreshToken: parsed.RefreshToken}
	if parsed.ExpiresIn > 0 {
		token.ExpiresAt = c.now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	}
	if parsed.RefreshTokenExpiresIn > 0 {
		token.RefreshTokenExpiresAt = c.now().Add(time.Duration(parsed.RefreshTokenExpiresIn) * time.Second)
	}
	return token, nil
}
