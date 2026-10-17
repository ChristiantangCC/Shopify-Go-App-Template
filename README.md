# Shopify Go App Template

> [!NOTE]
> This is a full multi-store Shopify app: merchants install it, it lives
> embedded in their admin, and it keeps working in the background. If you
> only need a backend for one store you control, use
> [Shopify-Go-Service-App-Template](https://github.com/ChristiantangCC/Shopify-Go-Service-App-Template)
> instead; it is a tenth of this.

One Go binary and one Postgres. The binary serves the embedded admin UI
(Preact + Polaris web components), its session-token-guarded API, the
webhook intake, App Proxy routes and the background workers. No Redis, no
second process.

What is already solved:

- **Install and auth.** Shopify managed install plus token exchange; there
  is no OAuth callback. Session tokens are verified on every API request.
- **Expiring offline tokens.** Stored encrypted (AES-256-GCM), refreshed
  before expiry behind a per-shop Postgres advisory lock, kept alive by a
  daily job, and a rejected refresh token routes the shop through a new
  token exchange on its next open.
- **Webhooks.** HMAC verification, dedupe on the delivery id and job
  enqueue in one transaction, then 200. A delivery is never processed
  twice and never lost after a 200. Handlers run in background jobs with
  retries.
- **Lifecycle.** app/uninstalled, app/scopes_update and the three
  compliance topics are handled out of the box.
- **Admin API client.** Throttle-aware with Shopify's cost feedback,
  retries that respect mutation safety (only `@idempotent` mutations are
  resent after a 5xx), and one forced token refresh on 401. Bulk
  operations included, webhook-driven.
- **Request logging.** One line per API request with a request id that
  follows a webhook into its background job.

## Stack

| Piece | Choice | Why |
|---|---|---|
| HTTP | chi | stdlib-shaped router, no framework |
| Database | pgx + sqlc + goose | typed queries generated from plain SQL, migrations in the repo, no ORM runtime |
| Jobs | River | jobs live in Postgres, so webhook dedupe + enqueue commit atomically and there is no Redis |
| Frontend | Vite + Preact + TanStack Query + Polaris web components | 22 KB gzipped, React mental model, Shopify's own components, typed by `@shopify/polaris-types` so a wrong prop fails the typecheck |
| GraphQL | genqlient codegen from `.graphql` documents | compile-time safety against the Admin schema; `bun run codegen` regenerates |

## Setup

You need Go 1.26+, Docker, [Bun](https://bun.sh) and Shopify CLI
(`npm install -g @shopify/cli`, or brew).

### 0. Make it yours

Either start from GitHub: **Use this template → Create a new repository**,
clone it, and rename it in place:

```bash
bun run new-app . github.com/you/my-app "My App"
```

Or stamp a sibling folder from a local checkout (no git history):

```bash
bun run new-app ../My-App github.com/you/my-app "My App"
```

Both rewrite the Go module path, the app name and handle in the toml
files and the frontend package, then verify the result builds.

### 1. Run it

```bash
bun run app:start:dev
```

That wraps `shopify app dev`, exactly like a Node Shopify template: on
first run the CLI logs you in, creates (or links) the dev app from the
terminal, then opens a tunnel, keeps the app's URLs pointed at it, asks
which dev store to use and installs the app there. The app side of it
(`scripts/app-dev.sh`, wired up in `shopify.web.toml`) starts Postgres
(auto-picking a free port when 5432 is taken), Vite hot reload and the Go
server, and persists the credentials the CLI hands over into `.env` so
tests and one-off scripts see the same values.

Linking a brand-new dev app makes the CLI rewrite `shopify.app.dev.toml`
from the Dev Dashboard, which would drop the template's webhooks and App
Proxy. `scripts/link-dev.sh` runs first and keeps them: it copies only the
new client id, name and handle into the file. Commit the result.

While it runs, use the CLI's keys: `p` opens the app in the dev store,
`g` opens GraphiQL against it, `q` quits.

### 2. See it work

Press `p`: the app opens embedded, the first open runs the token exchange
(`[Tokens] Install saved`) and syncs the dynamic webhook subscriptions
(`[Webhooks] Subscription created`). Rename any product in the store:
within seconds the log shows `[Webhooks] Delivery queued` and the worker's
`[Webhooks] Product updated`.

## Adding things

### A page

1. Create `web/frontend/src/pages/<name>.tsx` (copy `home.tsx`).
2. Add it to `ROUTES` in `web/frontend/src/routes.tsx`.

The router and the admin sidebar both render from `ROUTES`; there is no
third place.

### An API route

Add a handler in `internal/handlers/` and mount it inside the
`/api/admin` group in `cmd/server/main.go`. Everything in that group sits
behind session-token verification; read the shop with
`sessiontoken.SessionFrom(ctx)`. Call it from the frontend with
`authFetch`.

### A webhook

1. Write a `HandlerFunc` in `internal/shopify/webhooks/registry.go` (or a
   new file in that package).
2. Register it in `Registry`.
3. `SourceToml` topics (app-specific, same for every shop) also go into
   both toml files; run `bun run app:deploy:dev`. `SourceDynamic` topics need
   nothing else: the app syncs them per shop through the Admin API on
   install and on every app open.

Handlers run in River workers with two retries; return an error to retry a
delivery, nil to finish it.

### A job

Define an args type and a worker in `internal/jobs/jobs.go`, register the
worker in `NewClient`, and either enqueue it (`river.Client.Insert`, or
`InsertTx` to tie it to a database write) or add it to `PeriodicJobs`.

### An extension

Checkout, theme, admin and other Shopify extensions live in
`extensions/<name>`, where the Shopify CLI finds them:

```bash
bun run generate:extension
```

Pass the generator's flags through, e.g. `bun run generate:extension
--template checkout_ui --name upsell`. Don't run `shopify app generate
extension` directly: it installs the new extension's packages with npm,
which crashes on bun's `node_modules`. The script runs the generator in a
clean temporary folder and moves the extension in.

Each extension's `package.json` is a bun workspace, so one `bun install`
at the root installs everything, and the Docker build installs it too.
`bun run app:start:dev` builds and serves extensions alongside the app,
and `bun run app:deploy:dev` releases them.

### Per-shop settings

The Settings page is the pattern: a `shop_settings` row per shop
(cascading from `shops`, so `shop/redact` wipes it), sqlc queries, a
`GET`/`PUT /api/admin/settings` pair, and a native form with App Bridge's
`data-save-bar` giving dirty tracking and discard for free. Add fields by
extending the migration, the queries and the form.

### A table or a query

Migrations live in `internal/db/migrations` (goose, applied when the
server starts) and queries in `internal/store/queries` as plain SQL. After
changing either, regenerate the typed Go code in `internal/store/gen`:

```bash
bun run db:generate
```

It runs a pinned sqlc through `go run`, so nothing needs installing.

### A bulk operation

For "every record in the store" workloads, start a bulk query with
`bulk.Start` (see `POST /api/admin/bulk/products`). Shopify runs it
server-side and delivers `bulk_operations/finish`, a permanent dynamic
subscription in the registry; its handler fetches the result URL and
streams the JSONL line by line. Replace the demo handler's line counter
with your processing.

### A GraphQL operation

Operations are `.graphql` documents in
`internal/shopify/graphql/operations/`, one file per domain, compiled to
typed Go by genqlient. Add or change an operation, then:

```bash
bun run codegen
```

That fetches the Admin schema from shopify.dev's public introspection
proxy (no token needed; `SHOPIFY_API_VERSION` picks the version) and
regenerates `generated.go`, which is committed, so clones build without
running codegen. Call the generated function through the per-shop adapter:

```go
res, err := graphql.ProductsList(ctx, adminClient.ForShop(shop), 20, nil)
```

`ForShop` routes generated operations through the admin client, so token
refresh, throttle waits and retry rules all still apply. A typo or a
schema change fails at codegen or compile time instead of in production;
porting to API 2026-07 surfaced exactly that
(`WebhookSubscriptionInput.callbackUrl` became `uri`). Mark mutations that
are safe to resend with Shopify's `@idempotent(key: ...)` directive; the
client only retries 5xx/network failures for queries and idempotent
mutations (429 and THROTTLED always retry).

## Billing (optional)

Set `BILLING_ENABLED=true` and `SHOPIFY_APP_HANDLE=<handle>`. That mounts
`/api/admin/billing/status` and shows the Billing page: the live
subscription plus plan cards from `web/frontend/src/lib/plans.ts`.

The cards are placeholder UI and nothing is enforced: the server never
checks a plan, and no card creates a charge. The real plans are configured
as managed pricing in the Dev Dashboard, which you do when submitting the
app for review; the cards' Choose buttons open Shopify's hosted plan
picker, which is what actually changes a merchant's plan. Keep the catalog
mirroring that configuration, and add your own gating where your app needs
it. For the rare app that cannot use managed pricing, the generated
`AppSubscriptionCreate` operation ships too.

## Storefront and server-to-server

- **App Proxy** (`/proxy/...`): signed storefront requests, verified with
  the app secret, rate limited per customer or IP. Example: `/proxy/ping`.
- **API keys** (`/api/service/...`): mounted only when `API_KEY` or
  `API_KEYS` is set, for Shopify Flow, cron jobs or another backend.
  Example: `/api/service/products?shop=<shop>.myshopify.com`.

## Deploy

```bash
docker build -t my-app .
```

The image is distroless, runs as nonroot and listens on 8080. Give it the
variables from `.env.example`, a Postgres, and HTTPS termination in front
(Caddy, Traefik, a PaaS). Migrations run at boot.

The production app is its own entry in the Dev Dashboard, with its own
credentials, exactly like the dev app. Put its client id in
`shopify.app.toml`, point `application_url` and the proxy `url` at the
real domain, then release the config:

```bash
bun run app:deploy:prod
```

Same command whenever production scopes, webhook topics or URLs change.

## Commands

| Command | What it does |
|---|---|
| `bun run app:start:dev` | `shopify app dev` (tunnel, store, install, p/g hotkeys) plus Postgres, Vite and the Go server; `bun run dev` is an alias |
| `bun run generate:extension` | `shopify app generate extension` in a clean folder, then moves the extension in as a bun workspace |
| `bun run db:generate` | sqlc: regenerates `internal/store/gen` from the migrations and queries |
| `bun run app:deploy:dev` | push dev config (scopes, webhooks, URLs) to Shopify |
| `bun run app:deploy:prod` | release the production config |
| `bun run new-app <dir> <module> <name>` | stamp a new app from the template |
| `bun run test` | `go test ./...` |
| `bun run typecheck` | frontend type check |
| `bun run db:up` | just the dev Postgres |

## Tests

```bash
go test ./...
```

Unit tests cover every security piece: session token verification
(including alg confusion), webhook and App Proxy HMAC, token encryption,
API keys and the Admin client's retry rules. The integration tests (token
refresh single-flight under concurrency, install lifecycle, webhook
dedupe) run when `TEST_DATABASE_URL` points at a Postgres, which CI
provides; locally:

```bash
docker compose up -d postgres
TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/app?sslmode=disable go test ./...
```

## License

MIT
