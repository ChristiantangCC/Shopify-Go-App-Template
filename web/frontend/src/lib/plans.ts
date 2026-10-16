/**
 * Plan catalog: PLACEHOLDER UI ONLY. Nothing here is enforced anywhere;
 * the server never checks a plan, and these cards do not create charges.
 * The real plans are configured as managed pricing in the Dev Dashboard
 * (required before submitting the app for review); Shopify hosts the
 * actual plan picker and does the billing. Keep this catalog mirroring
 * that configuration, compare on the name Shopify reports, and replace
 * every price, quota, tagline and bullet with your app's real ones.
 */
export interface Plan {
  /** Must mirror the plan name configured in managed pricing. */
  name: string;
  /** Monthly USD price; null renders "Free". */
  priceUsd: number | null;
  tagline: string;
  /** The quota band, like "1,000 orders / month" or "No order limit". */
  quota: string;
  /** The line under the quota, like "$0.05 per extra order". */
  quotaDetail: string;
  features: string[];
}

export const PLANS: Plan[] = [
  {
    name: 'Free',
    priceUsd: null,
    tagline: 'Placeholder tagline for the Free plan.',
    quota: 'No limits yet',
    quotaDetail: 'Free forever',
    features: ['Feature one', 'Feature two'],
  },
  {
    name: 'Standard',
    priceUsd: 9,
    tagline: 'Placeholder tagline for the Standard plan.',
    quota: '1,000 things / month',
    quotaDetail: 'Upgrade when you outgrow it',
    features: ['Everything in Free', 'Feature three', 'Feature four'],
  },
  {
    name: 'Pro',
    priceUsd: 29,
    tagline: 'Placeholder tagline for the Pro plan.',
    quota: '10,000 things / month',
    quotaDetail: 'Upgrade when you outgrow it',
    features: ['Everything in Standard', 'Feature five', 'Feature six'],
  },
  {
    name: 'Scale',
    priceUsd: 99,
    tagline: 'Placeholder tagline for the Scale plan.',
    quota: 'Unlimited',
    quotaDetail: 'Fair use applies',
    features: ['Everything in Pro', 'Feature seven', 'Priority support'],
  },
];
