# App Proxy rate limit: verify `PROXY_TRUSTED_HOPS`

> **Unverified assumption.** The default `PROXY_TRUSTED_HOPS=2` has not
> been checked against a real Shopify App Proxy request. Confirm it on your
> deployment before you rely on the rate limit.

## What the setting does

`appproxy.RateLimitKey` keys anonymous storefront visitors by IP. The IP
comes from `X-Forwarded-For`, and a visitor can pre-fill that header with
anything, so the leftmost entry is never trusted. Each proxy in front of
the app appends the address it received the request from. The visitor's
real IP is the entry `PROXY_TRUSTED_HOPS` places from the right.

The default of 2 assumes this chain:

```
visitor → Shopify App Proxy → your reverse proxy (Traefik, nginx, …) → app
           appends visitor IP   appends Shopify's IP
```

which gives the app `X-Forwarded-For: <anything spoofed>, <visitor>, <shopify>`.

## The assumption

That Shopify's App Proxy **appends** the visitor's IP to `X-Forwarded-For`
when it forwards the request. Shopify's documentation doesn't confirm it.

If it's wrong, the limit misbehaves in one of two ways:

- **Shopify doesn't add the visitor's IP:** the entry two from the right is
  whatever the visitor sent (spoofable), or the header is too short and
  every anonymous visitor falls back to one shared bucket.
- **Shopify replaces the header instead of appending:** spoofed entries
  disappear, and the count may still be right, but confirm it.

Logged-in customers aren't affected: they're keyed on
`logged_in_customer_id` from the signed query string.

## How to verify

1. Temporarily log `r.Header.Values("X-Forwarded-For")` and
   `r.RemoteAddr` in `handlers.ProxyPing` and deploy. Nothing logs the
   header by default.
2. From a machine whose public IP you know, call the proxy through the
   storefront, once with a forged header:

   ```bash
   curl -H 'X-Forwarded-For: 6.6.6.6' https://<store>.myshopify.com/apps/<subpath>/ping
   ```

3. Read the logged header and count from the right to your own IP. That
   count is the correct `PROXY_TRUSTED_HOPS`. Check that `6.6.6.6` sits to
   the left of your IP, never at or right of it.
4. Set `PROXY_TRUSTED_HOPS` to that count, remove the temporary logging,
   and update this file with what you found.
