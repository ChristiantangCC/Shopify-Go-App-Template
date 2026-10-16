import { useQuery } from "@tanstack/react-query";

import { authFetch } from "../lib/auth-fetch";
import { PLANS, type Plan } from "../lib/plans";
import styles from "./billing.module.css";

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
  const response = await authFetch("/api/admin/billing/status");
  if (!response.ok) {
    throw new Error(`billing request failed: ${response.status}`);
  }
  return (await response.json()) as BillingStatus;
};

interface PlanCardProps {
  plan: Plan;
  isCurrent: boolean;
  /** Shopify's hosted plan-selection page; undefined until the app handle is configured. */
  pricingPlansUrl?: string;
}

const PlanCard = ({ plan, isCurrent, pricingPlansUrl }: PlanCardProps) => (
  // Four direct children = the four shared grid bands; a fifth breaks the
  // cross-card alignment (see billing.module.css).
  <div className={styles.planCard}>
    <s-stack direction="block" gap="small-500">
      <s-heading>{plan.name}</s-heading>
      <s-stack direction="inline" gap="small-500" alignItems="end">
        <s-text type="strong" fontSize="large-100">
          {plan.priceUsd === null ? "Free" : `$${plan.priceUsd}`}
        </s-text>
        {plan.priceUsd !== null && <s-text color="subdued">/ month</s-text>}
      </s-stack>
      <s-paragraph color="subdued">{plan.tagline}</s-paragraph>
    </s-stack>

    <div className={styles.planQuota}>
      <s-stack direction="block" gap="small-500">
        <s-text type="strong">{plan.quota}</s-text>
        <s-paragraph color="subdued">{plan.quotaDetail}</s-paragraph>
      </s-stack>
    </div>

    <div className={styles.planFeatures}>
      <s-stack direction="block" gap="small-400">
        {plan.features.map((feature) => (
          <div key={feature} className={styles.feature}>
            <span className={styles.featureIcon}>
              <s-icon type="check" tone="success" />
            </span>
            <span className={styles.featureText}>
              <s-text>{feature}</s-text>
            </span>
          </div>
        ))}
      </s-stack>
    </div>

    <div className={styles.planCta}>
      <s-button
        inlineSize="fill"
        variant="secondary"
        disabled={isCurrent || !pricingPlansUrl || undefined}
        onClick={pricingPlansUrl ? () => window.open(pricingPlansUrl, "_top") : undefined}
      >
        {isCurrent ? "Current plan" : "Switch plan"}
      </s-button>
    </div>
  </div>
);

/**
 * Plans & billing: plan cards projected from the catalog, the live
 * subscription from the billing endpoint. Switching plans happens on the
 * Shopify-hosted plan-selection page (managed pricing); this page is the
 * in-app view, and the catalog is placeholder UI with no enforcement.
 */
export const BillingPage = () => {
  const billing = useQuery({ queryKey: ["billing"], queryFn: fetchBillingStatus });

  const activeSubscription = billing.data?.subscriptions[0];
  const currentPlanName = activeSubscription?.name ?? "Free";

  return (
    <s-page heading="Plans & billing">
      <s-stack direction="block" gap="base">
        <s-banner heading="Template UI, nothing is enforced">
          These cards are placeholders: the server never checks a plan and no card creates a
          charge. Configure the real plans as managed pricing in the Dev Dashboard (required
          before app review) and keep web/frontend/src/lib/plans.ts mirroring them; the Switch
          buttons open Shopify's hosted plan picker, which is what actually changes a merchant's
          plan.
        </s-banner>

        {billing.isLoading && <s-spinner accessibilityLabel="Loading plan"></s-spinner>}
        {billing.isError && (
          <s-banner tone="critical" heading="Could not load billing">
            {String(billing.error)}
          </s-banner>
        )}

        {billing.data && (
          <div className={styles.planGrid}>
            {PLANS.map((plan) => (
              <PlanCard
                key={plan.name}
                plan={plan}
                isCurrent={plan.name === currentPlanName}
                pricingPlansUrl={billing.data.pricingPlansUrl}
              />
            ))}
          </div>
        )}

        {billing.data && !billing.data.pricingPlansUrl && (
          <s-paragraph color="subdued">
            Set SHOPIFY_APP_HANDLE in .env to enable the Switch buttons; they link to the
            managed-pricing plan picker.
          </s-paragraph>
        )}
        <s-paragraph color="subdued">
          All prices in USD, billed through the Shopify invoice.
        </s-paragraph>
      </s-stack>
    </s-page>
  );
};
