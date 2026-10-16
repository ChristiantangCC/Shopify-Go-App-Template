package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/admin"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/graphql"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/sessiontoken"
)

// BillingStatus reads the shop's active app subscriptions. The default
// billing path is Shopify's managed pricing: the response includes the
// pricing-plans page URL when the app handle is configured, and the
// frontend sends merchants there. appSubscriptionCreate (in
// internal/shopify/graphql/billing.go) exists for apps that cannot use
// managed pricing.
type BillingStatus struct {
	Admin     *admin.Client
	AppHandle string
	Logger    *slog.Logger
}

// ServeHTTP handles GET /api/admin/billing/status.
func (h BillingStatus) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	session, ok := sessiontoken.SessionFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	res, err := graphql.ActiveSubscriptions(r.Context(), h.Admin.ForShop(session.Shop))
	if err != nil {
		failAdmin(w, h.Logger, err)
		return
	}

	type subscription struct {
		ID               string `json:"id"`
		Name             string `json:"name"`
		Status           string `json:"status"`
		Test             bool   `json:"test"`
		CurrentPeriodEnd string `json:"currentPeriodEnd"`
	}
	subscriptions := make([]subscription, 0, len(res.CurrentAppInstallation.ActiveSubscriptions))
	for _, sub := range res.CurrentAppInstallation.ActiveSubscriptions {
		entry := subscription{ID: sub.Id, Name: sub.Name, Status: string(sub.Status), Test: sub.Test}
		if sub.CurrentPeriodEnd != nil {
			entry.CurrentPeriodEnd = sub.CurrentPeriodEnd.Format(time.RFC3339)
		}
		subscriptions = append(subscriptions, entry)
	}

	body := map[string]any{"subscriptions": subscriptions}
	if h.AppHandle != "" {
		storeHandle := strings.TrimSuffix(session.Shop, ".myshopify.com")
		body["pricingPlansUrl"] = fmt.Sprintf("https://admin.shopify.com/store/%s/charges/%s/pricing_plans", storeHandle, h.AppHandle)
	}
	writeJSON(w, http.StatusOK, body)
}
