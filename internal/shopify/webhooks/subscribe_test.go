package webhooks

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/admin"
)

type staticTokens struct{}

func (staticTokens) AccessToken(context.Context, string) (string, error) { return "tok", nil }
func (staticTokens) ForceRefresh(context.Context, string, string) (string, error) {
	return "tok", nil
}

// fakeAdmin answers the four webhook operations from a fixed subscription
// list and records every mutation as "Operation:arg".
type fakeAdmin struct {
	mu        sync.Mutex
	existing  []map[string]any
	mutations []string
}

func (f *fakeAdmin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	userErrors := []any{}
	var data map[string]any
	switch {
	case strings.Contains(req.Query, "query WebhookSubscriptionsList"):
		edges := []any{}
		for _, node := range f.existing {
			edges = append(edges, map[string]any{"node": node, "cursor": node["id"]})
		}
		data = map[string]any{"webhookSubscriptions": map[string]any{
			"edges":    edges,
			"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
		}}
	case strings.Contains(req.Query, "mutation WebhookSubscriptionCreate"):
		f.mutations = append(f.mutations, "create:"+req.Variables["topic"].(string))
		data = map[string]any{"webhookSubscriptionCreate": map[string]any{"webhookSubscription": map[string]any{"id": "new"}, "userErrors": userErrors}}
	case strings.Contains(req.Query, "mutation WebhookSubscriptionUpdate"):
		f.mutations = append(f.mutations, "update:"+req.Variables["id"].(string)+"->"+req.Variables["uri"].(string))
		data = map[string]any{"webhookSubscriptionUpdate": map[string]any{"webhookSubscription": map[string]any{"id": req.Variables["id"]}, "userErrors": userErrors}}
	case strings.Contains(req.Query, "mutation WebhookSubscriptionDelete"):
		f.mutations = append(f.mutations, "delete:"+req.Variables["id"].(string))
		data = map[string]any{"webhookSubscriptionDelete": map[string]any{"deletedWebhookSubscriptionId": req.Variables["id"], "userErrors": userErrors}}
	default:
		http.Error(w, "unexpected operation", http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func existingSub(id, topic, callbackURL string) map[string]any {
	return map[string]any{
		"id":       id,
		"topic":    topic,
		"endpoint": map[string]any{"__typename": "WebhookHttpEndpoint", "callbackUrl": callbackURL},
	}
}

func TestSyncReconcilesSubscriptions(t *testing.T) {
	const host = "https://app.example.com"
	fake := &fakeAdmin{existing: []map[string]any{
		// Wanted, but still pointing at an old dev tunnel: repoint.
		existingSub("gid://1", "PRODUCTS_CREATE", "https://old-tunnel.example.com/webhooks"),
		// Ours, but no longer in the registry: delete.
		existingSub("gid://2", "ORDERS_CREATE", host+"/webhooks"),
		// Wanted and correct: leave alone.
		existingSub("gid://3", "CUSTOMERS_UPDATE", host+"/webhooks"),
		// Not pointing at /webhooks, so not this sync's to touch.
		existingSub("gid://4", "ORDERS_PAID", host+"/elsewhere"),
	}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	saved := Registry
	t.Cleanup(func() { Registry = saved })
	never := func(context.Context, Deps, string) (bool, error) { return false, nil }
	Registry = []Registration{
		{Topic: "app/uninstalled", Source: SourceToml},
		{Topic: "products/create", Source: SourceDynamic},
		{Topic: "customers/update", Source: SourceDynamic},
		{Topic: "products/update", Source: SourceDynamic},
		{Topic: "orders/paid", Source: SourceDynamic, When: never},
	}

	client := admin.New("2026-10", staticTokens{}, admin.WithEndpoint(func(string) string { return server.URL }))
	subscriber := NewSubscriber(Deps{Admin: client, Logger: slog.New(slog.DiscardHandler)}, host)
	if err := subscriber.Sync(context.Background(), "example-shop.myshopify.com"); err != nil {
		t.Fatal(err)
	}

	slices.Sort(fake.mutations)
	want := []string{
		"create:PRODUCTS_UPDATE",
		"delete:gid://2",
		"update:gid://1->" + host + "/webhooks",
	}
	if !slices.Equal(fake.mutations, want) {
		t.Fatalf("mutations %v, want %v", fake.mutations, want)
	}
}
