package webhooks

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
)

// Source says how a topic gets subscribed.
type Source int

const (
	// SourceToml topics are app-specific subscriptions declared in
	// shopify.app.toml and delivered to every shop; `shopify app deploy`
	// registers them.
	SourceToml Source = iota
	// SourceDynamic topics are shop-specific subscriptions this app
	// creates at runtime through the Admin API (see subscribe.go).
	SourceDynamic
)

// Delivery is one verified webhook delivery.
type Delivery struct {
	Shop      string
	Topic     string
	WebhookID string
	Payload   json.RawMessage
}

// Deps is what handlers may touch. Keep it small: a handler that needs more
// should call into a service of its own.
type Deps struct {
	Store  *store.Store
	Logger *slog.Logger
}

// HandlerFunc processes one delivery inside a background job. Returning an
// error makes the queue retry with backoff.
type HandlerFunc func(ctx context.Context, deps Deps, delivery Delivery) error

// Registration wires one topic to its handler.
type Registration struct {
	// Topic in Shopify's path form, like "products/update".
	Topic  string
	Source Source
	Handle HandlerFunc
}

// Registry is the single place a webhook topic is added:
//
//  1. Write a HandlerFunc (below or in its own file).
//  2. Add a Registration here.
//  3. SourceToml topics also go into shopify.app.toml's [webhooks] section;
//     SourceDynamic topics are synced to every shop automatically.
var Registry = []Registration{
	{Topic: "app/uninstalled", Source: SourceToml, Handle: handleAppUninstalled},
	{Topic: "app/scopes_update", Source: SourceToml, Handle: handleScopesUpdate},
	{Topic: "customers/data_request", Source: SourceToml, Handle: handleCustomersDataRequest},
	{Topic: "customers/redact", Source: SourceToml, Handle: handleCustomersRedact},
	{Topic: "shop/redact", Source: SourceToml, Handle: handleShopRedact},

	// Examples of shop-specific subscriptions created via the Admin API.
	// products/create also powers the subscribe/unsubscribe demo on the
	// app's home page.
	{Topic: "products/update", Source: SourceDynamic, Handle: handleProductsUpdate},
	{Topic: "products/create", Source: SourceDynamic, Handle: handleProductsCreate},
}

// Lookup finds the registration for a topic.
func Lookup(topic string) (Registration, bool) {
	for _, registration := range Registry {
		if registration.Topic == topic {
			return registration, true
		}
	}
	return Registration{}, false
}

// DynamicTopics lists the topics subscribe.go keeps in sync per shop.
func DynamicTopics() []string {
	var topics []string
	for _, registration := range Registry {
		if registration.Source == SourceDynamic {
			topics = append(topics, registration.Topic)
		}
	}
	return topics
}

// handleAppUninstalled wipes the shop's tokens and marks it uninstalled.
// The row itself stays: Shopify expects shop data to survive until the
// shop/redact webhook arrives 48 hours later.
func handleAppUninstalled(ctx context.Context, deps Deps, delivery Delivery) error {
	deps.Logger.Info("[Webhooks] App uninstalled", "shop", delivery.Shop)
	return deps.Store.MarkUninstalled(ctx, delivery.Shop)
}

// handleScopesUpdate records the scope set the merchant now grants.
func handleScopesUpdate(ctx context.Context, deps Deps, delivery Delivery) error {
	var payload struct {
		Current []string `json:"current"`
	}
	if err := json.Unmarshal(delivery.Payload, &payload); err != nil {
		deps.Logger.Warn("[Webhooks] Unreadable scopes_update payload", "shop", delivery.Shop, "error", err)
		return nil
	}
	scopes := ""
	for i, scope := range payload.Current {
		if i > 0 {
			scopes += ","
		}
		scopes += scope
	}
	deps.Logger.Info("[Webhooks] Scopes updated", "shop", delivery.Shop, "scopes", scopes)
	return deps.Store.SetScopes(ctx, delivery.Shop, scopes)
}

// handleCustomersDataRequest must export the customer data the app stores
// and send it to the merchant within 30 days. This template stores no
// customer data, so acknowledging is enough; replace this when your app
// starts storing any.
func handleCustomersDataRequest(ctx context.Context, deps Deps, delivery Delivery) error {
	deps.Logger.Info("[Webhooks] Customer data requested, nothing stored", "shop", delivery.Shop)
	return nil
}

// handleCustomersRedact must delete the customer data the app stores. This
// template stores none; replace this when your app starts storing any.
func handleCustomersRedact(ctx context.Context, deps Deps, delivery Delivery) error {
	deps.Logger.Info("[Webhooks] Customer redact, nothing stored", "shop", delivery.Shop)
	return nil
}

// handleShopRedact arrives 48 hours after an uninstall and deletes
// everything about the shop. New tables should cascade from shops so this
// one delete covers them.
func handleShopRedact(ctx context.Context, deps Deps, delivery Delivery) error {
	deps.Logger.Info("[Webhooks] Shop redact", "shop", delivery.Shop)
	return deps.Store.Delete(ctx, delivery.Shop)
}

// handleProductsCreate is the second dynamic example; the home page's
// subscribe/unsubscribe buttons toggle this topic live.
func handleProductsCreate(ctx context.Context, deps Deps, delivery Delivery) error {
	var payload struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(delivery.Payload, &payload); err != nil {
		return err
	}
	deps.Logger.Info("[Webhooks] Product created", "shop", delivery.Shop, "productId", payload.ID, "title", payload.Title)
	return nil
}

// handleProductsUpdate is the dynamic-subscription example: replace its body
// with real work, or use it as the pattern for your own topics.
func handleProductsUpdate(ctx context.Context, deps Deps, delivery Delivery) error {
	var payload struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(delivery.Payload, &payload); err != nil {
		return err
	}
	deps.Logger.Info("[Webhooks] Product updated", "shop", delivery.Shop, "productId", payload.ID, "title", payload.Title)
	return nil
}
