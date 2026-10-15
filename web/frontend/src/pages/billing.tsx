import { useQuery } from '@tanstack/react-query';

import { authFetch } from '../lib/auth-fetch';

interface Subscription {
  id: string;
  name: string;
  status: string;
  test: boolean;
  currentPeriodEnd: string;
}

interface BillingStatus {
  subscriptions: Subscription[];
  pricingPlansUrl?: string;
}

const fetchBillingStatus = async (): Promise<BillingStatus> => {
  const response = await authFetch('/api/admin/billing/status');
  if (!response.ok) {
    throw new Error(`billing request failed: ${response.status}`);
  }
  return (await response.json()) as BillingStatus;
};

/**
 * Shows the shop's current subscription and links to the managed-pricing
 * plans page. This page only appears when BILLING_ENABLED is on.
 */
export const BillingPage = () => {
  const billing = useQuery({ queryKey: ['billing'], queryFn: fetchBillingStatus });

  return (
    <s-page heading="Billing">
      <s-section heading="Your plan">
        {billing.isLoading && <s-spinner accessibilityLabel="Loading plan"></s-spinner>}
        {billing.isError && (
          <s-banner tone="critical" heading="Could not load billing">
            {String(billing.error)}
          </s-banner>
        )}
        {billing.data && billing.data.subscriptions.length === 0 && (
          <s-paragraph>No active subscription: this shop is on the free plan.</s-paragraph>
        )}
        {billing.data?.subscriptions.map((subscription) => (
          <s-stack direction="inline" gap="base" alignItems="center">
            <s-text fontWeight="bold">{subscription.name}</s-text>
            <s-badge tone={subscription.status === 'ACTIVE' ? 'success' : 'neutral'}>
              {subscription.status.toLowerCase()}
            </s-badge>
            {subscription.test && <s-badge tone="info">test</s-badge>}
          </s-stack>
        ))}
        {billing.data?.pricingPlansUrl && (
          <s-button href={billing.data.pricingPlansUrl} target="_top" variant="primary">
            View plans
          </s-button>
        )}
      </s-section>
    </s-page>
  );
};
