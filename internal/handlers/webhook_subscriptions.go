package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/webhooks"
)

// SubscriptionState says, for every dynamic registry topic, whether the
// shop is subscribed.
type SubscriptionState struct {
	Topics map[string]bool `json:"topics" doc:"Topic (like products/create) to subscribed."`
}

// SubscriptionToggle subscribes or unsubscribes one dynamic topic.
type SubscriptionToggle struct {
	Topic      string `json:"topic" doc:"A dynamic registry topic, like products/create." example:"products/create"`
	Subscribed bool   `json:"subscribed"`
}

type subscriptionStateOutput struct {
	Body SubscriptionState
}

type subscriptionToggleInput struct {
	Body SubscriptionToggle
}

type subscriptionToggleOutput struct {
	Body SubscriptionToggle
}

// registerWebhookSubscriptions powers the home page's live
// subscribe/unsubscribe demo for dynamic topics. Only topics in the
// registry are allowed, so the endpoint can't point arbitrary
// subscriptions at the app.
func (h API) registerWebhookSubscriptions(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listWebhookSubscriptions",
		Method:      http.MethodGet,
		Path:        "/webhooks/subscriptions",
		Summary:     "The shop's subscription state for every dynamic topic",
		Errors:      []int{http.StatusBadGateway},
	}, func(ctx context.Context, _ *struct{}) (*subscriptionStateOutput, error) {
		session, err := sessionFrom(ctx)
		if err != nil {
			return nil, err
		}
		status, err := h.Subscriber.Subscribed(ctx, session.Shop)
		if err != nil {
			return nil, adminError(h.Logger, err)
		}
		return &subscriptionStateOutput{Body: SubscriptionState{Topics: status}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "toggleWebhookSubscription",
		Method:      http.MethodPost,
		Path:        "/webhooks/subscriptions",
		Summary:     "Subscribe or unsubscribe one dynamic topic",
		Errors:      []int{http.StatusBadRequest, http.StatusBadGateway},
	}, func(ctx context.Context, input *subscriptionToggleInput) (*subscriptionToggleOutput, error) {
		session, err := sessionFrom(ctx)
		if err != nil {
			return nil, err
		}
		toggle := input.Body
		registration, known := webhooks.Lookup(toggle.Topic)
		if !known || registration.Source != webhooks.SourceDynamic {
			return nil, huma.Error400BadRequest("not a dynamic registry topic")
		}
		if toggle.Subscribed {
			err = h.Subscriber.Subscribe(ctx, session.Shop, toggle.Topic)
		} else {
			err = h.Subscriber.Unsubscribe(ctx, session.Shop, toggle.Topic)
		}
		if err != nil {
			return nil, adminError(h.Logger, err)
		}
		return &subscriptionToggleOutput{Body: toggle}, nil
	})
}
