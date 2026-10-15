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
  resent after a 5xx), and one forced token refresh on 401.

## Stack

| Piece | Choice | Why |
|---|---|---|
| HTTP | chi | stdlib-shaped router, no framework |
| Database | pgx + sqlc + goose | typed queries generated from plain SQL, migrations in the repo, no ORM runtime |
| Jobs | River | jobs live in Postgres, so webhook dedupe + enqueue commit atomically and there is no Redis |
| Frontend | Vite + Preact + TanStack Query + Polaris web components | 22 KB gzipped, React mental model, Shopify's own components |
| GraphQL | documents + typed response structs in `internal/shopify/graphql/` | a template ships ten operations; codegen (genqlient) is overkill until you have dozens |

## Setup

You need Go 1.26+, Docker, pnpm and Shopify CLI
(`pnpm add -g @shopify/cli`).

### 0. Make it yours

Either start from GitHub: **Use this template → Create a new repository**,
clone it, and rename it in place:

```bash
pnpm new-app . github.com/you/my-app "My App"
```

Or stamp a sibling folder from a local checkout (no git history):

```bash
pnpm new-app ../My-App github.com/you/my-app "My App"
```

Both rewrite the Go module path, the app name and handle in the toml
files and the frontend package, then verify the result builds.

### 1. Run it

```bash
pnpm app:start:dev
```

That wraps `shopify app dev`, exactly like a Node Shopify template: on
first run the CLI logs you in, creates (or links) the dev app from the
terminal, then opens a tunnel, keeps the app's URLs pointed at it, asks
which dev store to use and installs the app there. The app side of it
(`scripts/app-dev.sh`, wired up in `shopify.web.toml`) starts Postgres
(auto-picking a free port when 5432 is taken), Vite hot reload and the Go
server, and persists the credentials the CLI hands over into `.env` so
tests and one-off scripts see the same values.

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
   both toml files; run `pnpm app:deploy:dev`. `SourceDynamic` topics need
   nothing else: the app syncs them per shop through the Admin API on
   install and on every app open.

Handlers run in River workers with two retries; return an error to retry a
delivery, nil to finish it.

### A job

Define an args type and a worker in `internal/jobs/jobs.go`, register the
worker in `NewClient`, and either enqueue it (`river.Client.Insert`, or
`InsertTx` to tie it to a database write) or add it to `PeriodicJobs`.

### A GraphQL operation

Put the document and its response struct in
`internal/shopify/graphql/<domain>.go`, then `admin.Client.Do(ctx, shop,
doc, vars, &out)`. Mark mutations that are safe to resend with Shopify's
`@idempotent(key: ...)` directive; the client only retries 5xx/network
failures for queries and idempotent mutations (429 and THROTTLED always
retry).

## Billing (optional)

Set `BILLING_ENABLED=true` and `SHOPIFY_APP_HANDLE=<handle>`. That mounts
`/api/admin/billing/status` and shows the Billing page, which reads the
active subscription and links to Shopify's managed pricing plans page.
Configure the plans themselves in the Dev Dashboard (managed pricing). For
the rare app that cannot use managed pricing, `graphql/billing.go` ships
`appSubscriptionCreate`.

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
pnpm app:deploy:prod
```

Same command whenever production scopes, webhook topics or URLs change.

## Commands

| Command | What it does |
|---|---|
| `pnpm app:start:dev` | `shopify app dev` (tunnel, store, install, p/g hotkeys) plus Postgres, Vite and the Go server; `pnpm dev` is an alias |
| `pnpm app:deploy:dev` | push dev config (scopes, webhooks, URLs) to Shopify |
| `pnpm app:deploy:prod` | release the production config |
| `pnpm new-app <dir> <module> <name>` | stamp a new app from the template |
| `pnpm test` | `go test ./...` |
| `pnpm typecheck` | frontend type check |
| `pnpm db:up` | just the dev Postgres |

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
