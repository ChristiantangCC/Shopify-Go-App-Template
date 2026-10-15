// Package shopdomain validates *.myshopify.com shop domains. Every place a
// shop name enters the app (session token dest, App Proxy query, CSP header)
// goes through Valid, so a crafted value can never reach a header or a URL.
package shopdomain

import "regexp"

var pattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.myshopify\.com$`)

// Valid reports whether shop is a plain myshopify.com domain, like
// "example-shop.myshopify.com".
func Valid(shop string) bool {
	return pattern.MatchString(shop)
}
