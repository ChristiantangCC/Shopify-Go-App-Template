import { describe, expect, it } from 'vitest';

import { fakeApi } from '../test/api';
import { shopifyMock } from '../test/setup';
import { api, apiError } from './api';
import { bootstrapSession } from './auth-fetch';

const session = { shop: 'example.myshopify.com', isNew: false, billingEnabled: false };
const settings = { notificationEmail: '', syncEnabled: true };

describe('api client', () => {
  it('sends a fresh session token as a bearer token', async () => {
    const calls = fakeApi({ 'GET /api/admin/settings': { body: settings } });

    const { data } = await api.GET('/api/admin/settings');

    expect(data).toEqual(settings);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.authorization).toBe('Bearer session-token-1');
  });

  it('sends the typed body as JSON', async () => {
    const calls = fakeApi({ 'PUT /api/admin/settings': { body: settings } });

    await api.PUT('/api/admin/settings', { body: settings });

    expect(calls[0]?.authorization).toBe('Bearer session-token-1');
    expect(JSON.parse(calls[0]?.body ?? '')).toEqual(settings);
  });

  it('re-runs the session bootstrap after a 401 and retries once, body included', async () => {
    let attempts = 0;
    const calls = fakeApi({
      'PUT /api/admin/settings': () => (++attempts === 1 ? { status: 401 } : { body: settings }),
      'POST /api/admin/session': { body: session },
    });
    shopifyMock.idToken
      .mockResolvedValueOnce('expired')
      .mockResolvedValueOnce('for-bootstrap')
      .mockResolvedValueOnce('for-retry');

    const { data } = await api.PUT('/api/admin/settings', { body: settings });

    expect(data).toEqual(settings);
    expect(calls.map((call) => `${call.method} ${call.path} ${call.authorization}`)).toEqual([
      'PUT /api/admin/settings Bearer expired',
      'POST /api/admin/session Bearer for-bootstrap',
      'PUT /api/admin/settings Bearer for-retry',
    ]);
    expect(calls[2]?.body).toBe(calls[0]?.body);
  });

  it('gives up after one retry instead of looping', async () => {
    const calls = fakeApi({
      'GET /api/admin/settings': { status: 401 },
      'POST /api/admin/session': { body: session },
    });

    const { error, response } = await api.GET('/api/admin/settings');

    expect(response.status).toBe(401);
    expect(error?.status).toBe(401);
    expect(calls.filter((call) => call.path === '/api/admin/settings')).toHaveLength(2);
  });

  it('turns other errors into the server’s message', async () => {
    const calls = fakeApi({
      'PUT /api/admin/settings': {
        status: 422,
        body: { title: 'Unprocessable Entity', status: 422, detail: 'bad email' },
      },
    });

    const { error, response } = await api.PUT('/api/admin/settings', { body: settings });

    expect(calls).toHaveLength(1);
    expect(apiError(error, response).message).toBe('bad email');
  });
});

describe('bootstrapSession', () => {
  it('returns the session info', async () => {
    fakeApi({ 'POST /api/admin/session': { body: session } });
    await expect(bootstrapSession()).resolves.toEqual(session);
  });

  it('throws when the server refuses, without bootstrapping again', async () => {
    const calls = fakeApi({ 'POST /api/admin/session': { status: 401 } });
    await expect(bootstrapSession()).rejects.toThrow('session bootstrap failed: 401');
    expect(calls).toHaveLength(1);
  });
});
