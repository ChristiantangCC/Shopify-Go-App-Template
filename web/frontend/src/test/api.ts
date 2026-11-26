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
 * answer 404 so a missing stub fails loudly. Returns the recorded calls.
 */
export const fakeApi = (
  routes: Record<string, FakeResponse | ((call: FakeCall) => FakeResponse)>,
): FakeCall[] => {
  const calls: FakeCall[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: string, init?: RequestInit) => {
      const headers = new Headers(init?.headers);
      const call: FakeCall = {
        method: init?.method ?? 'GET',
        path: input,
        authorization: headers.get('Authorization'),
        body: typeof init?.body === 'string' ? init.body : null,
      };
      calls.push(call);
      const route = routes[`${call.method} ${new URL(input, 'https://app.example.com').pathname}`];
      const answer = typeof route === 'function' ? route(call) : (route ?? { status: 404 });
      return new Response(JSON.stringify(answer.body ?? {}), {
        status: answer.status ?? 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }),
  );
  return calls;
};
