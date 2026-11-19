# Subscribing to changes: Events or webhooks

Shopify has two ways to tell the app something changed in a store. The
rule in this template is which shops need the subscription:

| Who needs it | Use | Declared in | Handler |
|---|---|---|---|
| **Every install** (static) | **Events** | `[events]` in `shopify.app.toml` | `internal/shopify/events/registry.go`, keyed by `handle` |
| **Only some shops** (dynamic: "shops that enabled X", "shops on plan Y") | **Webhooks via the GraphQL Admin API** | `SourceDynamic` entry with a `When` condition in `internal/shopify/webhooks/registry.go` | same registry entry |
| App lifecycle and GDPR compliance | Webhooks in TOML | `[webhooks]` in `shopify.app.toml` (already set up) | `SourceToml` entries in the webhooks registry |

## Why this split

**Events** (API `2026-10` and later) is Shopify's next-generation
mechanism and is set to replace webhooks over time. Compared with webhooks
it has:

- **Field-level triggers.** Subscribe to `product.variants.price`, not
  "anything on the product changed".
- **Your own payload.** Each subscription runs a GraphQL query, so the
  delivery carries exactly the fields the handler reads.
- **Query filters.** `query_filter = "product.status:'ACTIVE'"` drops
  deliveries the app doesn't care about before they're sent.

But Events can **only** be declared in `shopify.app.toml`. There is no API
to create one for a single shop, so every install gets every Events
subscription. That makes Events the right tool for static subscriptions
and the wrong one for conditional ones.

**Dynamic webhooks** are created per shop with `webhookSubscriptionCreate`,
so the app decides shop by shop. Use them when subscribing every shop
would be wasted work or wrong: deliveries for shops that turned a feature
off, or topics only a paid plan uses.

**Lifecycle and compliance topics** (`app/uninstalled`,
`app/scopes_update`, `customers/data_request`, `customers/redact`,
`shop/redact`) aren't commerce resources, and Events topics are (Product,
InventoryItem, Article, ...). They stay as TOML webhooks. The same goes for
any static need whose resource isn't in Events yet: check the topic list
in the [Events reference](https://shopify.dev/docs/api/events/2026-10),
and fall back to a `[[webhooks.subscriptions]]` entry until it is.

## Adding a static subscription (Events)

1. In **both** `shopify.app.toml` and `shopify.app.dev.toml`, uncomment the
   `[events]` block and write the subscription:

   ```toml
   [events]
   api_version = "2026-10"

     [[events.subscription]]
     handle = "product-price-changed"   # routes the delivery to its handler
     topic = "Product"                  # capitalized resource name
     actions = ["update"]               # create, update, delete
     triggers = ["product.variants.price"]  # required for update
     uri = "/events"                    # relative to application_url
     query = """
     query product_price_changed($productId: ID!) {
       product(id: $productId) { id title status }
     }
     """
     query_filter = "product.status:'ACTIVE'"   # optional
   ```

   The app's `[access_scopes]` must include the topic's scope
   (`read_products` for Product).

2. Register the handle in `internal/shopify/events/registry.go`:

   ```go
   var Registry = map[string]HandlerFunc{
   	"product-price-changed": handleProductPriceChanged,
   }
   ```

   `delivery.Data` holds your query's result; decode it into a struct of
   the same shape. `delivery.FieldsChanged` lists the paths that changed.

3. Push the config: `bun run app:deploy:dev`, and `bun run app:deploy:prod`
   for production. Shopify validates topics and triggers on deploy.

Deliveries arrive at `POST /events`. The intake verifies
`Shopify-Hmac-Sha256`, records `Shopify-Webhook-Id` so a retried delivery is
processed once, queues a River job and answers 200. The handler runs in
the job; returning an error retries it with backoff. A delivery whose
handle has no registry entry is logged and acknowledged, never retried.

## Adding a dynamic subscription (webhooks)

1. Add a registration with a condition in
   `internal/shopify/webhooks/registry.go`:

   ```go
   {Topic: "orders/create", Source: SourceDynamic, Handle: handleOrdersCreate, When: onPaidPlan},
   ```

   `When` gets the shop and returns whether it should be subscribed. Nil
   means every shop, but then ask whether it should be an Event instead.
   Keep conditions cheap (cached settings, not Admin API calls): they run
   on every sync.

2. Re-sync the shop whenever the condition's inputs change, by queueing
   `jobs.SyncWebhooksArgs{Shop: shop}`. Syncs already run on every app
   open. Saving Settings queues one too, which is how the `products/update`
   example follows the "sync enabled" toggle.

The subscriber only touches subscriptions pointing at this app's
`/webhooks` endpoint: it creates the ones a shop should have, repoints
ones whose URL changed (a new dev tunnel) and deletes the ones it
shouldn't have.
