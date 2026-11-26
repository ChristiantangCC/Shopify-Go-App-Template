// Package events handles Shopify Events deliveries: the next-generation
// subscription mechanism (Admin API 2026-10+), with field-level triggers,
// query filters and a custom GraphQL payload per subscription.
//
// Events are STATIC: they can only be declared in shopify.app.toml, so every
// install gets the same subscriptions. That makes them the default for "the
// app always needs to know when X changes". For subscriptions that depend on
// the shop ("only shops that enabled sync"), use dynamic webhooks instead;
// see docs/subscriptions.md for the decision rule.
//
// Deliveries arrive at POST /events (handlers.Events): HMAC-verified,
// deduplicated on Shopify-Webhook-Id and queued on River. The worker looks
// the subscription's handle up in Registry below and runs its handler.
package events

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/admin"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
)

// Delivery is one verified Events delivery.
type Delivery struct {
	// Shop is the shop's myshopify domain.
	Shop string
	// Topic is the capitalized resource, like "Product".
	Topic string
	// Action is "create", "update" or "delete".
	Action string
	// Handle is the subscription's handle from shopify.app.toml.
	Handle string
	// WebhookID is the delivery's Shopify-Webhook-Id.
	WebhookID string
	// Data is the result of the subscription's query, shaped by that query.
	Data json.RawMessage
	// FieldsChanged lists the field paths that triggered an update.
	FieldsChanged FieldsChanged
	// QueryVariables are the variables Shopify ran the query with, like
	// {"productId": "gid://shopify/Product/123"}.
	QueryVariables map[string]any
}

// FieldsChanged holds the field paths an update touched, like
// "product[id: 'gid://shopify/Product/1'].variants[id: '...'].price".
type FieldsChanged struct {
	Added   []string `json:"added"`
	Updated []string `json:"updated"`
	Removed []string `json:"removed"`
}

// Payload is the delivery body Shopify sends.
type Payload struct {
	Topic          string          `json:"topic"`
	Action         string          `json:"action"`
	Handle         string          `json:"handle"`
	Data           json.RawMessage `json:"data"`
	FieldsChanged  FieldsChanged   `json:"fields_changed"`
	QueryVariables map[string]any  `json:"query_variables"`
}

// Deps is what handlers may touch.
type Deps struct {
	Store  *store.Store
	Admin  *admin.Client
	Logger *slog.Logger
}

// HandlerFunc processes one delivery inside a background job. Returning an
// error makes the queue retry with backoff (up to 10 attempts); wrap it in
// retry.Permanent (internal/retry) when resending can never succeed, and
// the job is cancelled instead.
type HandlerFunc func(ctx context.Context, deps Deps, delivery Delivery) error

// Registry maps a subscription handle (the `handle` of an
// [[events.subscription]] in shopify.app.toml) to its handler. It is empty
// on purpose: the template subscribes to no Events yet.
//
// To add one:
//
//  1. Uncomment the [events] block in shopify.app.toml and
//     shopify.app.dev.toml, and write the subscription: a handle, a topic
//     (Product, Order, ...), actions, triggers for updates, uri = "/events",
//     and a query returning exactly the fields the handler needs.
//  2. Add the handle here with a handler. Decode delivery.Data into a
//     struct matching your query's shape.
//  3. Deploy the config: bun run app:deploy:dev (or app:deploy:prod).
//
// Example:
//
//	"product-price-changed": func(ctx context.Context, deps Deps, d Delivery) error {
//		var data struct {
//			Product struct {
//				ID    string `json:"id"`
//				Title string `json:"title"`
//			} `json:"product"`
//		}
//		if err := json.Unmarshal(d.Data, &data); err != nil {
//			return retry.Permanent(err)
//		}
//		deps.Logger.Info("[Events] Price changed", "shop", d.Shop, "product", data.Product.ID)
//		return nil
//	},
var Registry = map[string]HandlerFunc{}

// Lookup finds the handler for a subscription handle.
func Lookup(handle string) (HandlerFunc, bool) {
	handler, ok := Registry[handle]
	return handler, ok
}
