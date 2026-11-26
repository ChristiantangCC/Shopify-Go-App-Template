import createClient from 'openapi-fetch';

import type { components, paths } from '../api/schema';

/** What POST /api/admin/session answers. */
export type SessionInfo = components['schemas']['SessionInfo'];

/** Puts a fresh App Bridge session token on the request. */
const withToken = async (request: Request): Promise<Request> => {
  request.headers.set('Authorization', `Bearer ${await shopify.idToken()}`);
  return request;
};

/**
 * The bootstrap's own client: it attaches a session token but never
 * re-runs the bootstrap on a 401, which would loop.
 */
const sessionClient = createClient<paths>({
  fetch: async (request) => fetch(await withToken(request)),
});

/**
 * Tells the server this shop is open: it verifies the session token and
 * runs a token exchange when the shop has no usable offline token. Called
 * on app start and again whenever an API call answers 401.
 */
export const bootstrapSession = async (): Promise<SessionInfo> => {
  const { data, response } = await sessionClient.POST('/api/admin/session');
  if (!data) {
    throw new Error(`session bootstrap failed: ${response.status}`);
  }
  return data;
};

/**
 * The fetch behind the typed API client (lib/api.ts): it sends a fresh
 * session token, and on a 401 (the shop's offline token needs a new
 * exchange) it re-runs the session bootstrap and retries once.
 */
export const authFetch = async (request: Request): Promise<Response> => {
  // A request body can only be read once; keep a copy for the retry.
  const retry = request.clone();
  const response = await fetch(await withToken(request));
  if (response.status !== 401) {
    return response;
  }
  await bootstrapSession();
  return fetch(await withToken(retry));
};
