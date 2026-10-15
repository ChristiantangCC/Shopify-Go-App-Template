// Package jobs runs background work on River: jobs live in Postgres, are
// enqueued transactionally (the webhook intake inserts its job in the same
// transaction as the dedupe claim) and retry with backoff. The workers run
// inside the server binary; no extra process or broker exists.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/tokens"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/webhooks"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
	storegen "github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store/gen"
)

// webhookDeliveryRetention is how long processed delivery ids are kept for
// dedupe; Shopify retries a failed delivery for up to 48 hours.
const webhookDeliveryRetention = 72 * time.Hour

// refreshAhead is how close a refresh token's 90-day deadline may come
// before the keep-alive job rotates the pair.
const refreshAhead = 7 * 24 * time.Hour

// WebhookArgs processes one verified webhook delivery.
type WebhookArgs struct {
	Shop      string          `json:"shop"`
	Topic     string          `json:"topic"`
	WebhookID string          `json:"webhook_id"`
	Payload   json.RawMessage `json:"payload"`
}

// Kind names the job type in the river_job table.
func (WebhookArgs) Kind() string { return "webhook-delivery" }

// WebhookWorker dispatches a delivery to its registry handler.
type WebhookWorker struct {
	river.WorkerDefaults[WebhookArgs]
	deps webhooks.Deps
}

// Work looks the topic up in the registry and runs its handler.
func (w *WebhookWorker) Work(ctx context.Context, job *river.Job[WebhookArgs]) error {
	registration, ok := webhooks.Lookup(job.Args.Topic)
	if !ok {
		w.deps.Logger.Warn("[Jobs] No handler for topic", "topic", job.Args.Topic)
		return nil
	}
	return registration.Handle(ctx, w.deps, webhooks.Delivery{
		Shop:      job.Args.Shop,
		Topic:     job.Args.Topic,
		WebhookID: job.Args.WebhookID,
		Payload:   job.Args.Payload,
	})
}

// SyncWebhooksArgs reconciles one shop's dynamic subscriptions.
type SyncWebhooksArgs struct {
	Shop string `json:"shop"`
}

// Kind names the job type.
func (SyncWebhooksArgs) Kind() string { return "sync-webhooks" }

// SyncWebhooksWorker runs the subscriber for one shop.
type SyncWebhooksWorker struct {
	river.WorkerDefaults[SyncWebhooksArgs]
	subscriber *webhooks.Subscriber
}

// Work syncs the shop's subscriptions.
func (w *SyncWebhooksWorker) Work(ctx context.Context, job *river.Job[SyncWebhooksArgs]) error {
	return w.subscriber.Sync(ctx, job.Args.Shop)
}

// TokenKeepAliveArgs rotates token pairs whose refresh token nears expiry,
// so a shop that nobody opens for months keeps a working offline token.
type TokenKeepAliveArgs struct{}

// Kind names the job type.
func (TokenKeepAliveArgs) Kind() string { return "token-keep-alive" }

// TokenKeepAliveWorker refreshes every shop the deadline approaches for.
type TokenKeepAliveWorker struct {
	river.WorkerDefaults[TokenKeepAliveArgs]
	store  *store.Store
	tokens *tokens.Service
	logger *slog.Logger
}

// Work refreshes each matching shop; one failing shop does not stop the rest.
func (w *TokenKeepAliveWorker) Work(ctx context.Context, job *river.Job[TokenKeepAliveArgs]) error {
	shops, err := w.store.ListShopsWithExpiringRefreshTokens(ctx, time.Now().Add(refreshAhead))
	if err != nil {
		return err
	}
	for _, shop := range shops {
		if err := w.tokens.Refresh(ctx, shop); err != nil {
			w.logger.Warn("[Jobs] Keep-alive refresh failed", "shop", shop, "error", err)
		}
	}
	return nil
}

// PruneWebhookDeliveriesArgs clears old dedupe rows.
type PruneWebhookDeliveriesArgs struct{}

// Kind names the job type.
func (PruneWebhookDeliveriesArgs) Kind() string { return "prune-webhook-deliveries" }

// PruneWebhookDeliveriesWorker deletes dedupe rows past retention.
type PruneWebhookDeliveriesWorker struct {
	river.WorkerDefaults[PruneWebhookDeliveriesArgs]
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// Work prunes and logs how many rows went.
func (w *PruneWebhookDeliveriesWorker) Work(ctx context.Context, job *river.Job[PruneWebhookDeliveriesArgs]) error {
	pruned, err := storegen.New(w.pool).PruneWebhookDeliveries(ctx, time.Now().Add(-webhookDeliveryRetention))
	if err != nil {
		return err
	}
	if pruned > 0 {
		w.logger.Info("[Jobs] Pruned webhook deliveries", "rows", pruned)
	}
	return nil
}

// Options wires NewClient.
type Options struct {
	Pool       *pgxpool.Pool
	Store      *store.Store
	Tokens     *tokens.Service
	Subscriber *webhooks.Subscriber
	Logger     *slog.Logger
}

// NewClient builds the River client with every worker and periodic job
// registered. The caller starts it with Start and stops it on shutdown.
func NewClient(opts Options) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &WebhookWorker{deps: webhooks.Deps{Store: opts.Store, Logger: opts.Logger}})
	river.AddWorker(workers, &SyncWebhooksWorker{subscriber: opts.Subscriber})
	river.AddWorker(workers, &TokenKeepAliveWorker{store: opts.Store, tokens: opts.Tokens, logger: opts.Logger})
	river.AddWorker(workers, &PruneWebhookDeliveriesWorker{pool: opts.Pool, logger: opts.Logger})

	client, err := river.NewClient(riverpgxv5.New(opts.Pool), &river.Config{
		Logger: opts.Logger,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
		},
		Workers: workers,
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(
				river.PeriodicInterval(24*time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return TokenKeepAliveArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true},
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return PruneWebhookDeliveriesArgs{}, nil },
				nil,
			),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("jobs: %w", err)
	}
	return client, nil
}
