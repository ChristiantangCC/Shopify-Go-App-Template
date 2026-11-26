// Package jobs runs background work on River: jobs live in Postgres, are
// enqueued transactionally (the webhook intake inserts its job in the same
// transaction as the dedupe claim) and retry with backoff. The workers run
// inside the server binary; no extra process or broker exists.
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/logging"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/retry"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/admin"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/events"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/tokens"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/webhooks"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
)

// webhookDeliveryRetention is how long processed delivery ids are kept for
// dedupe; Shopify retries a failed delivery for up to 48 hours.
const webhookDeliveryRetention = 72 * time.Hour

// refreshAhead is how close a refresh token's 90-day deadline may come
// before the keep-alive job rotates the pair.
const refreshAhead = 7 * 24 * time.Hour

// Attempt limits per job kind. River's default (25, with attempt^4 second
// backoff) keeps retrying for weeks; these stop much sooner. With that
// backoff, 10 attempts span roughly three hours.
const (
	deliveryMaxAttempts = 10
	syncMaxAttempts     = 5
	periodicMaxAttempts = 3
)

// syncWindow is how long one app open's webhook sync covers later opens of
// the same shop: opening the app repeatedly queues one sync, not one each.
const syncWindow = 10 * time.Minute

// finish turns a handler's error into the job's outcome: retry.Permanent
// errors cancel the job, everything else retries with backoff.
func finish(err error) error {
	if retry.IsPermanent(err) {
		return river.JobCancel(err)
	}
	return err
}

// WebhookArgs processes one verified webhook delivery. RequestID is the
// intake request's id, so the HTTP log line and the job's log line connect.
type WebhookArgs struct {
	Shop      string          `json:"shop"`
	Topic     string          `json:"topic"`
	WebhookID string          `json:"webhook_id"`
	RequestID string          `json:"request_id"`
	Payload   json.RawMessage `json:"payload"`
}

// Kind names the job type in the river_job table.
func (WebhookArgs) Kind() string { return "webhook-delivery" }

// InsertOpts caps retries for every webhook delivery job.
func (WebhookArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: deliveryMaxAttempts}
}

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
	w.deps.Logger.Info("[Jobs] Webhook delivery", "topic", job.Args.Topic, "shop", job.Args.Shop, "req", logging.ShortID(job.Args.RequestID), "attempt", job.Attempt)
	return finish(registration.Handle(ctx, w.deps, webhooks.Delivery{
		Shop:      job.Args.Shop,
		Topic:     job.Args.Topic,
		WebhookID: job.Args.WebhookID,
		Payload:   job.Args.Payload,
	}))
}

// EventArgs processes one verified Events delivery. Payload is the raw body.
type EventArgs struct {
	Shop      string          `json:"shop"`
	Topic     string          `json:"topic"`
	Action    string          `json:"action"`
	Handle    string          `json:"handle"`
	WebhookID string          `json:"webhook_id"`
	RequestID string          `json:"request_id"`
	Payload   json.RawMessage `json:"payload"`
}

// Kind names the job type.
func (EventArgs) Kind() string { return "event-delivery" }

// InsertOpts caps retries for every Events delivery job.
func (EventArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: deliveryMaxAttempts}
}

// EventWorker dispatches an Events delivery to its registry handler.
type EventWorker struct {
	river.WorkerDefaults[EventArgs]
	deps events.Deps
}

// Work decodes the delivery and runs the handler registered for its handle.
func (w *EventWorker) Work(ctx context.Context, job *river.Job[EventArgs]) error {
	handler, ok := events.Lookup(job.Args.Handle)
	if !ok {
		w.deps.Logger.Warn("[Jobs] No handler for event handle", "handle", job.Args.Handle)
		return nil
	}
	var payload events.Payload
	if err := json.Unmarshal(job.Args.Payload, &payload); err != nil {
		// A body that won't decode will never decode: don't retry.
		w.deps.Logger.Error("[Jobs] Unreadable event payload", "handle", job.Args.Handle, "shop", job.Args.Shop, "error", err)
		return nil
	}
	w.deps.Logger.Info("[Jobs] Event delivery", "handle", job.Args.Handle, "topic", job.Args.Topic, "action", job.Args.Action, "shop", job.Args.Shop, "req", logging.ShortID(job.Args.RequestID), "attempt", job.Attempt)
	return finish(handler(ctx, w.deps, events.Delivery{
		Shop:           job.Args.Shop,
		Topic:          job.Args.Topic,
		Action:         job.Args.Action,
		Handle:         job.Args.Handle,
		WebhookID:      job.Args.WebhookID,
		Data:           payload.Data,
		FieldsChanged:  payload.FieldsChanged,
		QueryVariables: payload.QueryVariables,
	}))
}

// SyncWebhooksArgs reconciles one shop's dynamic subscriptions.
type SyncWebhooksArgs struct {
	Shop string `json:"shop"`
	// Host is the callback base URL the sync points subscriptions at. It
	// is part of the args so the app-open dedupe (OncePerWindow) treats a
	// new dev tunnel as a new sync instead of skipping it.
	Host string `json:"host,omitempty"`
}

// Kind names the job type.
func (SyncWebhooksArgs) Kind() string { return "sync-webhooks" }

// InsertOpts caps retries for webhook syncs.
func (SyncWebhooksArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: syncMaxAttempts}
}

// OncePerWindow is the insert option for syncs triggered by an app open:
// the same shop (and host) gets at most one sync per syncWindow, however
// often the merchant opens the app. Syncs after a settings change insert
// without it, because they must run.
func OncePerWindow() *river.InsertOpts {
	return &river.InsertOpts{
		MaxAttempts: syncMaxAttempts,
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByPeriod: syncWindow},
	}
}

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

// InsertOpts caps retries; the next daily run picks up whatever failed.
func (TokenKeepAliveArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: periodicMaxAttempts}
}

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

// reencryptBatch is how many shops the re-encrypt job lists per query.
const reencryptBatch = 100

// ReencryptTokensArgs rewrites tokens sealed with an older encryption key
// (ENCRYPTION_KEYS_OLD) with the current one. It runs at every boot, so a
// deploy with a new ENCRYPTION_KEY re-encrypts straight away, and daily
// after that.
type ReencryptTokensArgs struct{}

// Kind names the job type.
func (ReencryptTokensArgs) Kind() string { return "reencrypt-tokens" }

// InsertOpts caps retries; the next run picks up whatever is left.
func (ReencryptTokensArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: periodicMaxAttempts}
}

// ReencryptTokensWorker pages through shops with stale tokens and reseals
// each under its lock. One failing shop (say, sealed with a key that is no
// longer configured) is logged and skipped.
type ReencryptTokensWorker struct {
	river.WorkerDefaults[ReencryptTokensArgs]
	store  *store.Store
	keyID  string
	logger *slog.Logger
}

// Work reseals every stale shop and logs the outcome: "0 left" means the
// old keys can be removed from ENCRYPTION_KEYS_OLD.
func (w *ReencryptTokensWorker) Work(ctx context.Context, job *river.Job[ReencryptTokensArgs]) error {
	var afterID int64
	resealed, failed := 0, 0
	for {
		shops, err := w.store.ListShopsWithStaleTokens(ctx, afterID, reencryptBatch)
		if err != nil {
			return err
		}
		for _, shop := range shops {
			afterID = shop.ID
			changed, err := w.store.ResealTokens(ctx, shop.ShopDomain)
			if err != nil {
				failed++
				w.logger.Error("[Jobs] Re-encrypt failed", "shop", shop.ShopDomain, "error", err)
				continue
			}
			if changed {
				resealed++
			}
		}
		if len(shops) < reencryptBatch {
			break
		}
	}
	if resealed > 0 || failed > 0 {
		w.logger.Info("[Jobs] Tokens re-encrypted", "key", w.keyID, "resealed", resealed, "failed", failed)
	}
	if failed == 0 {
		w.logger.Info("[Jobs] Every token is sealed with the current key", "key", w.keyID)
	}
	return nil
}

// PruneWebhookDeliveriesArgs clears old dedupe rows.
type PruneWebhookDeliveriesArgs struct{}

// Kind names the job type.
func (PruneWebhookDeliveriesArgs) Kind() string { return "prune-webhook-deliveries" }

// InsertOpts caps retries; the next hourly run picks up whatever failed.
func (PruneWebhookDeliveriesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: periodicMaxAttempts}
}

// PruneWebhookDeliveriesWorker deletes dedupe rows past retention.
type PruneWebhookDeliveriesWorker struct {
	river.WorkerDefaults[PruneWebhookDeliveriesArgs]
	store  *store.Store
	logger *slog.Logger
}

// Work prunes and logs how many rows went.
func (w *PruneWebhookDeliveriesWorker) Work(ctx context.Context, job *river.Job[PruneWebhookDeliveriesArgs]) error {
	pruned, err := w.store.PruneWebhookDeliveries(ctx, time.Now().Add(-webhookDeliveryRetention))
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
	Admin      *admin.Client
	Tokens     *tokens.Service
	Subscriber *webhooks.Subscriber
	// KeyID is the current encryption key's id, for the re-encrypt logs.
	KeyID  string
	Logger *slog.Logger
}

// NewClient builds the River client with every worker and periodic job
// registered. The caller starts it with Start and stops it on shutdown.
func NewClient(opts Options) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &WebhookWorker{deps: webhooks.Deps{Store: opts.Store, Admin: opts.Admin, Logger: opts.Logger}})
	river.AddWorker(workers, &EventWorker{deps: events.Deps{Store: opts.Store, Admin: opts.Admin, Logger: opts.Logger}})
	river.AddWorker(workers, &SyncWebhooksWorker{subscriber: opts.Subscriber})
	river.AddWorker(workers, &TokenKeepAliveWorker{store: opts.Store, tokens: opts.Tokens, logger: opts.Logger})
	river.AddWorker(workers, &PruneWebhookDeliveriesWorker{store: opts.Store, logger: opts.Logger})
	river.AddWorker(workers, &ReencryptTokensWorker{store: opts.Store, keyID: opts.KeyID, logger: opts.Logger})

	client, err := river.NewClient(riverpgxv5.New(opts.Pool), &river.Config{
		// River's own bookkeeping (producer counts, leadership) logs at
		// INFO every few seconds; warnings and errors still get through.
		Logger: logging.WithMinLevel(opts.Logger, slog.LevelWarn),
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
			river.NewPeriodicJob(
				river.PeriodicInterval(24*time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return ReencryptTokensArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true},
			),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("jobs: %w", err)
	}
	return client, nil
}

// NewInserter builds an insert-only River client on database/sql. It exists
// so a job can be enqueued inside a GORM transaction (River's InsertTx takes
// the *sql.Tx under it, see store.SQLTx); it never works jobs itself, the
// client from NewClient does. Both write to the same river_job table.
func NewInserter(sqlDB *sql.DB, logger *slog.Logger) (*river.Client[*sql.Tx], error) {
	client, err := river.NewClient(riverdatabasesql.New(sqlDB), &river.Config{
		Logger: logging.WithMinLevel(logger, slog.LevelWarn),
	})
	if err != nil {
		return nil, fmt.Errorf("jobs: inserter: %w", err)
	}
	return client, nil
}
