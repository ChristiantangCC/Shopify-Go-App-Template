import { describe, expect, it } from 'vitest';

import { fakeApi } from '../test/api';
import { shopifyMock } from '../test/setup';
import { authFetch, bootstrapSession } from './auth-fetch';

const session = { shop: 'example.myshopify.com', isNew: false, billingEnabled: false };

describe('authFetch', () => {
  it('sends a fresh session token as a bearer token', async () => {
    const calls = fakeApi({ 'GET /api/admin/settings': { body: {} } });

    const response = await authFetch('/api/admin/settings');

    expect(response.status).toBe(200);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.authorization).toBe('Bearer session-token-1');
  });

  it('keeps the caller’s own headers', async () => {
    const calls = fakeApi({ 'PUT /api/admin/settings': { body: {} } });

    await authFetch('/api/admin/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: '{}',
    });

    expect(calls[0]?.authorization).toBe('Bearer session-token-1');
    expect(calls[0]?.body).toBe('{}');
  });

  it('re-runs the session bootstrap after a 401 and retries once with a new token', async () => {
    let attempts = 0;
    const calls = fakeApi({
      'GET /api/admin/products': () => (++attempts === 1 ? { status: 401 } : { body: {} }),
      'POST /api/admin/session': { body: session },
    });
    shopifyMock.idToken
      .mockResolvedValueOnce('expired')
      .mockResolvedValueOnce('for-bootstrap')
      .mockResolvedValueOnce('for-retry');

    const response = await authFetch('/api/admin/products');

    expect(response.status).toBe(200);
    expect(calls.map((call) => `${call.method} ${call.path} ${call.authorization}`)).toEqual([
      'GET /api/admin/products Bearer expired',
      'POST /api/admin/session Bearer for-bootstrap',
      'GET /api/admin/products Bearer for-retry',
    ]);
  });

  it('gives up after one retry instead of looping', async () => {
    const calls = fakeApi({
      'GET /api/admin/products': { status: 401 },
      'POST /api/admin/session': { body: session },
    });

    const response = await authFetch('/api/admin/products');

    expect(response.status).toBe(401);
    expect(calls.filter((call) => call.path === '/api/admin/products')).toHaveLength(2);
  });

  it('passes other errors straight through', async () => {
    const calls = fakeApi({ 'GET /api/admin/products': { status: 502 } });

    const response = await authFetch('/api/admin/products');

    expect(response.status).toBe(502);
    expect(calls).toHaveLength(1);
  });
});

describe('bootstrapSession', () => {
  it('returns the session info', async () => {
    fakeApi({ 'POST /api/admin/session': { body: session } });
    await expect(bootstrapSession()).resolves.toEqual(session);
  });

  it('throws when the server refuses', async () => {
    fakeApi({ 'POST /api/admin/session': { status: 502 } });
    await expect(bootstrapSession()).rejects.toThrow('session bootstrap failed: 502');
  });
});
