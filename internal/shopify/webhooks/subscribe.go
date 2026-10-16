package webhooks

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/admin"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/graphql"
)

// Subscriber keeps a shop's dynamic webhook subscriptions matching the
// registry: it creates missing ones, repoints ones whose callback URL
// changed (a new dev tunnel), and deletes ones the registry dropped. It only
// ever touches subscriptions that point at this app's /webhooks endpoint.
type Subscriber struct {
	admin  *admin.Client
	host   string
	logger *slog.Logger
}

// NewSubscriber wires a Subscriber. host is the app's public base URL.
func NewSubscriber(adminClient *admin.Client, host string, logger *slog.Logger) *Subscriber {
	return &Subscriber{admin: adminClient, host: host, logger: logger}
}

// callbackURL is where every subscription points.
func (s *Subscriber) callbackURL() string { return s.host + "/webhooks" }

// Sync reconciles the shop's subscriptions with DynamicTopics. It is
// idempotent and safe to run on every install and app open.
func (s *Subscriber) Sync(ctx context.Context, shop string) error {
	wanted := map[string]bool{}
	for _, topic := range DynamicTopics() {
		wanted[topicEnum(topic)] = true
	}

	existing, err := s.list(ctx, shop)
	if err != nil {
		return err
	}

	for _, sub := range existing {
		mine := strings.HasSuffix(sub.callbackURL, "/webhooks")
		if !mine {
			continue
		}
		switch {
		case wanted[sub.topic] && sub.callbackURL == s.callbackURL():
			delete(wanted, sub.topic)
		case wanted[sub.topic]:
			if err := s.update(ctx, shop, sub.id); err != nil {
				return err
			}
			s.logger.Info("[Webhooks] Subscription repointed", "shop", shop, "topic", sub.topic)
			delete(wanted, sub.topic)
		default:
			if err := s.delete(ctx, shop, sub.id); err != nil {
				return err
			}
			s.logger.Info("[Webhooks] Subscription removed", "shop", shop, "topic", sub.topic)
		}
	}

	for topic := range wanted {
		if err := s.create(ctx, shop, topic); err != nil {
			return err
		}
		s.logger.Info("[Webhooks] Subscription created", "shop", shop, "topic", topic)
	}
	return nil
}

// Subscribe creates the shop-specific subscription for one topic (path
// form, like "products/create"). Already subscribed is a no-op.
func (s *Subscriber) Subscribe(ctx context.Context, shop, topic string) error {
	existing, err := s.list(ctx, shop)
	if err != nil {
		return err
	}
	enum := topicEnum(topic)
	for _, sub := range existing {
		if sub.topic == enum && sub.callbackURL == s.callbackURL() {
			return nil
		}
	}
	if err := s.create(ctx, shop, enum); err != nil {
		return err
	}
	s.logger.Info("[Webhooks] Subscription created", "shop", shop, "topic", enum)
	return nil
}

// Unsubscribe deletes the shop's subscription for one topic. Not being
// subscribed is a no-op.
func (s *Subscriber) Unsubscribe(ctx context.Context, shop, topic string) error {
	existing, err := s.list(ctx, shop)
	if err != nil {
		return err
	}
	enum := topicEnum(topic)
	for _, sub := range existing {
		if sub.topic == enum && strings.HasSuffix(sub.callbackURL, "/webhooks") {
			if err := s.delete(ctx, shop, sub.id); err != nil {
				return err
			}
			s.logger.Info("[Webhooks] Subscription removed", "shop", shop, "topic", enum)
		}
	}
	return nil
}

// Subscribed reports, for every registry dynamic topic, whether the shop
// currently has the subscription.
func (s *Subscriber) Subscribed(ctx context.Context, shop string) (map[string]bool, error) {
	existing, err := s.list(ctx, shop)
	if err != nil {
		return nil, err
	}
	active := map[string]bool{}
	for _, sub := range existing {
		if strings.HasSuffix(sub.callbackURL, "/webhooks") {
			active[sub.topic] = true
		}
	}
	status := map[string]bool{}
	for _, topic := range DynamicTopics() {
		status[topic] = active[topicEnum(topic)]
	}
	return status, nil
}

type subscription struct {
	id          string
	topic       string
	callbackURL string
}

func (s *Subscriber) list(ctx context.Context, shop string) ([]subscription, error) {
	client := s.admin.ForShop(shop)
	var subs []subscription
	var after *string
	for {
		res, err := graphql.WebhookSubscriptionsList(ctx, client, 100, after)
		if err != nil {
			return nil, fmt.Errorf("webhooks: list: %w", err)
		}
		for _, edge := range res.WebhookSubscriptions.Edges {
			sub := subscription{id: edge.Node.Id, topic: string(edge.Node.Topic)}
			if endpoint, ok := edge.Node.Endpoint.(*graphql.WebhookSubscriptionEndpointWebhookHttpEndpoint); ok {
				sub.callbackURL = endpoint.CallbackUrl
			}
			subs = append(subs, sub)
		}
		if !res.WebhookSubscriptions.PageInfo.HasNextPage {
			return subs, nil
		}
		after = res.WebhookSubscriptions.PageInfo.EndCursor
	}
}

func (s *Subscriber) create(ctx context.Context, shop, topicEnum string) error {
	res, err := graphql.WebhookSubscriptionCreate(ctx, s.admin.ForShop(shop), graphql.WebhookSubscriptionTopic(topicEnum), s.callbackURL())
	if err != nil {
		return fmt.Errorf("webhooks: create %s: %w", topicEnum, err)
	}
	if errs := res.WebhookSubscriptionCreate.UserErrors; len(errs) > 0 {
		return fmt.Errorf("webhooks: create %s: %s", topicEnum, errs[0].Message)
	}
	return nil
}

func (s *Subscriber) update(ctx context.Context, shop, id string) error {
	res, err := graphql.WebhookSubscriptionUpdate(ctx, s.admin.ForShop(shop), id, s.callbackURL())
	if err != nil {
		return fmt.Errorf("webhooks: update: %w", err)
	}
	if errs := res.WebhookSubscriptionUpdate.UserErrors; len(errs) > 0 {
		return fmt.Errorf("webhooks: update: %s", errs[0].Message)
	}
	return nil
}

func (s *Subscriber) delete(ctx context.Context, shop, id string) error {
	res, err := graphql.WebhookSubscriptionDelete(ctx, s.admin.ForShop(shop), id)
	if err != nil {
		return fmt.Errorf("webhooks: delete: %w", err)
	}
	if errs := res.WebhookSubscriptionDelete.UserErrors; len(errs) > 0 {
		return fmt.Errorf("webhooks: delete: %s", errs[0].Message)
	}
	return nil
}

// topicEnum converts "products/update" to the GraphQL enum PRODUCTS_UPDATE.
func topicEnum(topic string) string {
	return strings.ToUpper(strings.NewReplacer("/", "_", ".", "_").Replace(topic))
}
