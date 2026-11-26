import { waitFor } from '@testing-library/preact';
import { describe, expect, it } from 'vitest';

import { App } from './app';
import { fakeApi } from './test/api';
import { renderWithQuery } from './test/render';

const homeApi = (billingEnabled: boolean) => ({
  'POST /api/admin/session': {
    body: { shop: 'example.myshopify.com', isNew: false, billingEnabled },
  },
  'GET /api/admin/webhooks/subscriptions': { body: { topics: { 'products/create': false } } },
});

const navLabels = (container: Element) =>
  Array.from(container.querySelectorAll('ui-nav-menu a')).map((link) => link.textContent);

describe('App', () => {
  it('bootstraps the session, then renders the nav and the home page', async () => {
    const calls = fakeApi(homeApi(false));
    const { container } = renderWithQuery(<App />);

    expect(container.querySelector('s-spinner')).not.toBeNull();
    await waitFor(() =>
      expect(container.querySelector('s-page[heading="Shopify Go App Template"]')).not.toBeNull(),
    );

    expect(calls[0]?.method).toBe('POST');
    expect(calls[0]?.path).toBe('/api/admin/session');
    expect(navLabels(container)).toEqual(['Home', 'Settings']);
  });

  it('adds the billing page to the nav only when billing is enabled', async () => {
    fakeApi(homeApi(true));
    const { container } = renderWithQuery(<App />);

    await waitFor(() =>
      expect(navLabels(container)).toEqual(['Home', 'Settings', 'Plans & Billing']),
    );
  });

  it('shows an error instead of a blank page when the bootstrap fails', async () => {
    fakeApi({ 'POST /api/admin/session': { status: 502 } });
    const { container } = renderWithQuery(<App />);

    // The bootstrap retries once (about a second) before giving up.
    await waitFor(
      () =>
        expect(container.querySelector('s-banner')?.getAttribute('heading')).toBe(
          'The app could not start',
        ),
      { timeout: 3000 },
    );
  });
});
