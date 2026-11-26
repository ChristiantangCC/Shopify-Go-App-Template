import { vi } from 'vitest';

/** One fake API answer: a status and a JSON body. */
export interface FakeResponse {
  status?: number;
  body?: unknown;
}

/** A recorded call to the fake API. */
export interface FakeCall {
  method: string;
  path: string;
  authorization: string | null;
  body: string | null;
}

/**
 * Stubs fetch with a fake app API. routes maps "METHOD /path" to an answer,
 * or to a function of the call for answers that change. Unknown routes
 * answer 404 so a missing stub fails loudly. Errors use the API's
 * problem+json shape. Returns the recorded calls.
 */
export const fakeApi = (
  routes: Record<string, FakeResponse | ((call: FakeCall) => FakeResponse)>,
): FakeCall[] => {
  const calls: FakeCall[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: Request | string, init?: RequestInit) => {
      const request =
        typeof input === 'string' ? new Request(new URL(input, location.href), init) : input;
      const url = new URL(request.url);
      const text = await request.text();
      const call: FakeCall = {
        method: request.method,
        path: url.pathname + url.search,
        authorization: request.headers.get('Authorization'),
        body: text === '' ? null : text,
      };
      calls.push(call);
      const route = routes[`${call.method} ${url.pathname}`];
      const answer = typeof route === 'function' ? route(call) : (route ?? { status: 404 });
      const status = answer.status ?? 200;
      const body =
        answer.body ?? (status >= 400 ? { title: 'Error', status, detail: `fake ${status}` } : {});
      return new Response(JSON.stringify(body), {
        status,
        headers: {
          'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
        },
      });
    }),
  );
  return calls;
};
