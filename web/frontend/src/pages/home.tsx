import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ComponentChildren } from 'preact';

import { authFetch } from '../lib/auth-fetch';

const code = {
  fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
  fontSize: '0.85em',
  background: 'rgba(0, 0, 0, 0.06)',
  borderRadius: '4px',
  padding: '1px 6px',
};

const Path = ({ children }: { children: string }) => <span style={code}>{children}</span>;

interface FeatureProps {
  title: string;
  badge?: string;
  children: ComponentChildren;
}

const Feature = ({ title, badge, children }: FeatureProps) => (
  <div>
    <s-stack direction="inline" gap="small-200" alignItems="center">
      <s-text fontWeight="bold">{title}</s-text>
      {badge && <s-badge tone="info">{badge}</s-badge>}
    </s-stack>
    <s-paragraph color="subdued">{children}</s-paragraph>
  </div>
);

interface SubscriptionsResponse {
  topics: Record<string, boolean>;
}

const fetchSubscriptions = async (): Promise<SubscriptionsResponse> => {
  const response = await authFetch('/api/admin/webhooks/subscriptions');
  if (!response.ok) {
    throw new Error(`subscriptions request failed: ${response.status}`);
  }
  return (await response.json()) as SubscriptionsResponse;
};

/**
 * Live demo: toggles the products/create subscription through the real
 * webhookSubscriptionCreate / webhookSubscriptionDelete mutations.
 */
const DynamicSubscriptionDemo = () => {
  const demoTopic = 'products/create';
  const queryClient = useQueryClient();
  const subscriptions = useQuery({ queryKey: ['webhook-subscriptions'], queryFn: fetchSubscriptions });

  const toggle = useMutation({
    mutationFn: async (subscribed: boolean) => {
      const response = await authFetch('/api/admin/webhooks/subscriptions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ topic: demoTopic, subscribed }),
      });
      if (!response.ok) {
        throw new Error(`toggle failed: ${response.status}`);
      }
      return subscribed;
    },
    onSuccess: (subscribed) => {
      shopify.toast.show(subscribed ? `Subscribed to ${demoTopic}` : `Unsubscribed from ${demoTopic}`);
      queryClient.invalidateQueries({ queryKey: ['webhook-subscriptions'] });
    },
    onError: (error) => {
      shopify.toast.show(String(error), { isError: true });
    },
  });

  const subscribed = subscriptions.data?.topics[demoTopic];

  return (
    <s-stack direction="inline" gap="base" alignItems="center">
      <Path>products/create</Path>
      {subscriptions.isLoading && <s-spinner accessibilityLabel="Loading subscription state" size="small"></s-spinner>}
      {subscriptions.data && (
        <s-badge tone={subscribed ? 'success' : 'neutral'}>
          {subscribed ? 'subscribed' : 'not subscribed'}
        </s-badge>
      )}
      <s-button
        variant="primary"
        disabled={toggle.isPending || subscribed === true}
        onClick={() => toggle.mutate(true)}
      >
        Subscribe
      </s-button>
      <s-button
        disabled={toggle.isPending || subscribed === false}
        onClick={() => toggle.mutate(false)}
      >
        Unsubscribe
      </s-button>
    </s-stack>
  );
};

/** The template's front page: what ships, where to change it, and a live demo. */
export const HomePage = () => {
  return (
    <s-page heading="Shopify Go App Template">
      <s-section heading="You are looking at a working app">
        <s-paragraph>
          One Go binary and one Postgres. This page runs embedded in the admin behind session-token
          auth, the install already exchanged tokens, and the background workers are processing
          jobs. Everything below is wired and tested; replace the examples with your app.
        </s-paragraph>
        <s-stack direction="inline" gap="small-200">
          <s-badge>Go + chi</s-badge>
          <s-badge>Postgres + sqlc</s-badge>
          <s-badge>River jobs</s-badge>
          <s-badge>Preact + TanStack Query</s-badge>
          <s-badge>Polaris web components</s-badge>
        </s-stack>
      </s-section>

      <s-section heading="Auth and tokens: already handled">
        <s-stack direction="block" gap="base">
          <Feature title="Managed install + token exchange">
            No OAuth callback exists. Opening the app sends an App Bridge session token to{' '}
            <Path>POST /api/admin/session</Path>, which exchanges it for an expiring offline token
            when the shop needs one. Every <Path>/api/admin</Path> route verifies the session token
            in middleware.
          </Feature>
          <Feature title="Expiring offline tokens">
            Access and refresh tokens live encrypted (AES-256-GCM) in Postgres. Refresh happens
            automatically five minutes before expiry behind a per-shop advisory lock, so concurrent
            requests and multiple instances refresh exactly once. A daily job keeps rarely-opened
            shops alive. You just call <Path>admin.Client.Do</Path>.
          </Feature>
        </s-stack>
      </s-section>

      <s-section heading="Webhooks: one registry, two subscription kinds">
        <s-paragraph>
          Every topic is wired in exactly one place,{' '}
          <Path>internal/shopify/webhooks/registry.go</Path>: a handler function plus a line in the
          registry. The <Path>Source</Path> field decides how it gets subscribed.
        </s-paragraph>
        <s-stack direction="block" gap="base">
          <Feature title="Static (app-specific)" badge="SourceToml">
            Declared in <Path>shopify.app.toml</Path> and registered by{' '}
            <Path>pnpm app:deploy:dev</Path>. Same for every shop; this is how{' '}
            <Path>app/uninstalled</Path>, <Path>app/scopes_update</Path> and the three compliance
            topics ship.
          </Feature>
          <Feature title="Dynamic (shop-specific)" badge="SourceDynamic">
            Created per shop at runtime through <Path>webhookSubscriptionCreate</Path>. Try it: the
            buttons below create and delete the <Path>products/create</Path> subscription against
            this store, live. Then create a product and watch the server log print{' '}
            <Path>[Webhooks] Product created</Path>. Unsubscribe and the log stays quiet. And
            because the topic is in the registry, reopening the app runs the reconciler and
            subscribes it again; that is the self-healing behavior that also repoints callbacks
            when your dev tunnel URL changes.
          </Feature>
          <DynamicSubscriptionDemo />
          <Feature title="Delivery pipeline">
            HMAC check on the raw body, then one Postgres transaction claims the delivery id and
            enqueues the job, then 200. Duplicates are absorbed, and a failed enqueue rolls back so
            Shopify retries. A delivery is never lost after a 200 and never processed twice.
          </Feature>
        </s-stack>
      </s-section>

      <s-section heading="Background jobs: River on Postgres">
        <s-stack direction="block" gap="base">
          <Feature title="No Redis">
            Jobs live in the same Postgres as your data, which is what makes the transactional
            webhook handoff possible. Workers run inside the server binary with retries and
            backoff; nothing extra to deploy.
          </Feature>
          <Feature title="Adding a job">
            Define an args type and a worker in <Path>internal/jobs/jobs.go</Path>, register it in{' '}
            <Path>NewClient</Path>, then <Path>Insert</Path> it, or <Path>InsertTx</Path> to commit
            it atomically with a database write. Periodic jobs (the token keep-alive, dedupe
            pruning) are a list entry in the same file.
          </Feature>
        </s-stack>
      </s-section>

      <s-section heading="Calling the Admin API">
        <s-paragraph>
          Operations are plain GraphQL documents with typed response structs, one file per domain
          in <Path>internal/shopify/graphql/</Path>. The client retries THROTTLED and 429 using
          Shopify's cost feedback, retries 5xx only for queries and mutations marked{' '}
          <Path>@idempotent(key:)</Path>, and recovers from a 401 with one forced token refresh.
          The example endpoint <Path>GET /api/admin/products</Path> shows the full path from{' '}
          <Path>authFetch</Path> to a typed GraphQL response.
        </s-paragraph>
      </s-section>

      <s-section heading="Where to go from here">
        <s-unordered-list>
          <s-list-item>
            Add a page: a component in <Path>web/frontend/src/pages/</Path> plus one entry in{' '}
            <Path>src/routes.tsx</Path>. The router and the admin nav both read that registry.
          </s-list-item>
          <s-list-item>
            Add an API route: a handler in <Path>internal/handlers/</Path>, mounted under{' '}
            <Path>/api/admin</Path> in <Path>cmd/server/main.go</Path>, called with{' '}
            <Path>authFetch</Path>.
          </s-list-item>
          <s-list-item>
            Explore the API: press <Path>g</Path> in the dev terminal for GraphiQL against this
            store.
          </s-list-item>
          <s-list-item>
            Storefront and server-to-server: signed App Proxy routes under <Path>/proxy</Path>, and
            API-key routes under <Path>/api/service</Path> when keys are configured.
          </s-list-item>
          <s-list-item>
            Billing: set <Path>BILLING_ENABLED=true</Path> and a Billing page appears in the nav,
            reading the live subscription and linking to managed pricing.
          </s-list-item>
        </s-unordered-list>
        <s-paragraph color="subdued">
          The README covers each recipe in detail. This page is yours to delete.
        </s-paragraph>
      </s-section>
    </s-page>
  );
};
