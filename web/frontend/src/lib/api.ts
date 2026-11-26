import createClient from 'openapi-fetch';

import type { components, paths } from '../api/schema';
import { authFetch } from './auth-fetch';

/**
 * The typed client for the app's own API. Paths, query parameters, bodies
 * and responses all come from api/openapi.yaml, which the Go server's huma
 * operations generate: change an API type in Go, run `bun run codegen:api`,
 * and every mismatched call here fails the typecheck.
 *
 *   const { data, error, response } = await api.GET('/api/admin/settings');
 *
 * Every request carries a session token and retries once after a 401.
 */
export const api = createClient<paths>({ fetch: authFetch });

/** A schema from the spec, by name: Schema<'Settings'>. */
export type Schema<Name extends keyof components['schemas']> = components['schemas'][Name];

/** The problem+json body every API error carries. */
type ApiError = Schema<'ErrorModel'>;

/**
 * Turns an API call's error into an Error with the server's message, for
 * TanStack Query and toasts. Use as: if (error) throw apiError(error, response).
 */
export const apiError = (error: ApiError | undefined, response: Response): Error =>
  new Error(error?.detail ?? error?.title ?? `request failed: ${response.status}`);
