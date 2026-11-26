import { fireEvent, waitFor } from '@testing-library/preact';
import { describe, expect, it } from 'vitest';

import { fakeApi } from '../test/api';
import { renderWithQuery } from '../test/render';
import { SettingsPage } from './settings';

// Polaris fields are web components loaded from Shopify's CDN, which the
// tests don't load: they render as plain elements, Preact sets their props
// as attributes (HTML lowercases the names), and these tests check those
// attributes rather than Polaris behaviour.
describe('SettingsPage', () => {
  it('fills the form with the saved settings', async () => {
    fakeApi({
      'GET /api/admin/settings': {
        body: { notificationEmail: 'ops@example.com', syncEnabled: false },
      },
    });
    const { container } = renderWithQuery(<SettingsPage />);

    await waitFor(() =>
      expect(container.querySelector('s-email-field')?.getAttribute('defaultvalue')).toBe(
        'ops@example.com',
      ),
    );
    expect(container.querySelector('s-checkbox')?.hasAttribute('defaultchecked')).toBe(false);
  });

  it('flags an invalid email while typing and clears it once valid', async () => {
    fakeApi({ 'GET /api/admin/settings': { body: { notificationEmail: '', syncEnabled: true } } });
    const { container } = renderWithQuery(<SettingsPage />);
    const email = await waitFor(() => {
      const field = container.querySelector('s-email-field');
      expect(field).not.toBeNull();
      return field as HTMLElement & { value: string };
    });

    email.value = 'not-an-email';
    fireEvent.input(email);
    await waitFor(() => expect(email.getAttribute('error')).toBe('Enter a valid email address'));

    email.value = 'ops@example.com';
    fireEvent.input(email);
    await waitFor(() => expect(email.hasAttribute('error')).toBe(false));
  });

  it('shows an error banner when the settings can’t be loaded', async () => {
    fakeApi({ 'GET /api/admin/settings': { status: 500 } });
    const { container } = renderWithQuery(<SettingsPage />);

    await waitFor(() =>
      expect(container.querySelector('s-banner')?.getAttribute('heading')).toBe(
        'Could not load settings',
      ),
    );
  });
});
