import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'preact/hooks';

import { authFetch } from '../lib/auth-fetch';

interface Settings {
  notificationEmail: string;
  syncEnabled: boolean;
}

const fetchSettings = async (): Promise<Settings> => {
  const response = await authFetch('/api/admin/settings');
  if (!response.ok) {
    throw new Error(`settings request failed: ${response.status}`);
  }
  return (await response.json()) as Settings;
};

const saveSettings = async (settings: Settings): Promise<Settings> => {
  const response = await authFetch('/api/admin/settings', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settings),
  });
  if (!response.ok) {
    const body = (await response.json().catch(() => null)) as { error?: string } | null;
    throw new Error(body?.error ?? `save failed: ${response.status}`);
  }
  return (await response.json()) as Settings;
};

/**
 * Inline validation, shown while the merchant types. The server validates
 * again on save; this only mirrors it for fast feedback.
 */
const emailError = (value: string): string | undefined => {
  if (value === '' || /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)) {
    return undefined;
  }
  return 'Enter a valid email address';
};

/**
 * The per-shop settings pattern: a native form with App Bridge's
 * data-save-bar (dirty tracking and discard for free), saved through
 * PUT /api/admin/settings into the shop_settings table. Copy this page
 * when your app needs configuration.
 */
export const SettingsPage = () => {
  const queryClient = useQueryClient();
  const settings = useQuery({ queryKey: ['settings'], queryFn: fetchSettings });
  const [fieldError, setFieldError] = useState<string | undefined>(undefined);

  const save = useMutation({
    mutationFn: saveSettings,
    onSuccess: (saved) => {
      // Fresh data re-keys the form below, so the inputs' defaults match
      // the saved state and the save bar disappears.
      queryClient.setQueryData(['settings'], saved);
      shopify.toast.show('Settings saved');
    },
    onError: (error) => {
      shopify.toast.show(String(error), { isError: true });
    },
  });

  const onSubmit = (event: Event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget as HTMLFormElement);
    const next = {
      notificationEmail: String(form.get('notificationEmail') ?? ''),
      syncEnabled: form.get('syncEnabled') !== null,
    };
    const error = emailError(next.notificationEmail);
    setFieldError(error);
    if (error) {
      return;
    }
    save.mutate(next);
  };

  return (
    <s-page heading="Settings">
      <s-section heading="App settings">
        {settings.isLoading && <s-spinner accessibilityLabel="Loading settings"></s-spinner>}
        {settings.isError && (
          <s-banner tone="critical" heading="Could not load settings">
            {String(settings.error)}
          </s-banner>
        )}
        {settings.data && (
          <form data-save-bar onSubmit={onSubmit} key={JSON.stringify(settings.data)}>
            <s-stack direction="block" gap="base">
              <s-email-field
                label="Notification email"
                name="notificationEmail"
                defaultValue={settings.data.notificationEmail}
                details="Where this app sends its notifications. Leave empty to disable."
                error={fieldError}
                onInput={(event) => setFieldError(emailError(event.currentTarget.value))}
              ></s-email-field>
              <s-checkbox
                label="Sync enabled"
                name="syncEnabled"
                defaultChecked={settings.data.syncEnabled}
                details="Example toggle; wire it to your own logic."
              ></s-checkbox>
            </s-stack>
          </form>
        )}
      </s-section>
      <s-section heading="How this works">
        <s-paragraph color="subdued">
          Edit a field and the admin's save bar appears: App Bridge watches any form with the
          data-save-bar attribute, Save submits it and Discard resets it. The submit handler PUTs
          to /api/admin/settings, the server validates and upserts shop_settings (one row per
          shop, cascading on shop/redact), and the saved values come back as the form's new
          baseline.
        </s-paragraph>
      </s-section>
    </s-page>
  );
};
