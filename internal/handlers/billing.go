package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/graphql"
)

// AppSubscription is one of the shop's active app subscriptions.
type AppSubscription struct {
	ID               string `json:"id"`
	Name             string `json:"name" doc:"The plan name configured in managed pricing."`
	Status           string `json:"status"`
	Test             bool   `json:"test"`
	CurrentPeriodEnd string `json:"currentPeriodEnd" doc:"RFC 3339; empty when Shopify reports none."`
}

// BillingStatus is the shop's billing state.
type BillingStatus struct {
	Subscriptions   []AppSubscription `json:"subscriptions"`
	PricingPlansURL string            `json:"pricingPlansUrl,omitempty" doc:"Shopify's hosted plan picker; absent until SHOPIFY_APP_HANDLE is set."`
}

type billingStatusOutput struct {
	Body BillingStatus
}

// registerBilling adds GET /api/admin/billing/status, mounted only with
// BILLING_ENABLED. The default billing path is Shopify's managed pricing:
// the response includes the pricing-plans page URL when the app handle is
// configured, and the frontend sends merchants there.
// appSubscriptionCreate (internal/shopify/graphql/operations/billing.graphql)
// exists for apps that cannot use managed pricing.
func (h API) registerBilling(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getBillingStatus",
		Method:      http.MethodGet,
		Path:        "/billing/status",
		Summary:     "The shop's active app subscriptions",
		Errors:      []int{http.StatusBadGateway},
	}, func(ctx context.Context, _ *struct{}) (*billingStatusOutput, error) {
		session, err := sessionFrom(ctx)
		if err != nil {
			return nil, err
		}
		res, err := graphql.ActiveSubscriptions(ctx, h.Admin.ForShop(session.Shop))
		if err != nil {
			return nil, adminError(h.Logger, err)
		}
		status := BillingStatus{Subscriptions: make([]AppSubscription, 0, len(res.CurrentAppInstallation.ActiveSubscriptions))}
		for _, sub := range res.CurrentAppInstallation.ActiveSubscriptions {
			entry := AppSubscription{ID: sub.Id, Name: sub.Name, Status: string(sub.Status), Test: sub.Test}
			if sub.CurrentPeriodEnd != nil {
				entry.CurrentPeriodEnd = sub.CurrentPeriodEnd.Format(time.RFC3339)
			}
			status.Subscriptions = append(status.Subscriptions, entry)
		}
		if h.AppHandle != "" {
			storeHandle := strings.TrimSuffix(session.Shop, ".myshopify.com")
			status.PricingPlansURL = fmt.Sprintf("https://admin.shopify.com/store/%s/charges/%s/pricing_plans", storeHandle, h.AppHandle)
		}
		return &billingStatusOutput{Body: status}, nil
	})
}
