/**
 * Calls the app's own API with a fresh App Bridge session token. On a 401
 * the shop's offline token needs a new exchange, so it re-runs the session
 * bootstrap and retries once.
 */
export const authFetch = async (input: string, init?: RequestInit): Promise<Response> => {
  const response = await fetch(input, await withToken(init));
  if (response.status !== 401) {
    return response;
  }
  await bootstrapSession();
  return fetch(input, await withToken(init));
};

/**
 * Tells the server this shop is open: it verifies the session token and
 * runs a token exchange when the shop has no usable offline token. Called
 * on app start and again whenever an API call answers 401.
 */
export const bootstrapSession = async (): Promise<SessionInfo> => {
  const response = await fetch('/api/admin/session', { method: 'POST', ...(await withToken()) });
  if (!response.ok) {
    throw new Error(`session bootstrap failed: ${response.status}`);
  }
  return (await response.json()) as SessionInfo;
};

/** What POST /api/admin/session answers. */
export interface SessionInfo {
  shop: string;
  isNew: boolean;
  billingEnabled: boolean;
}

const withToken = async (init?: RequestInit): Promise<RequestInit> => {
  const token = await shopify.idToken();
  return {
    ...init,
    headers: { ...init?.headers, Authorization: `Bearer ${token}` },
  };
};
