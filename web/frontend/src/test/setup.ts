import { cleanup } from '@testing-library/preact';
import { afterEach, beforeEach, vi } from 'vitest';

/** The parts of App Bridge's `shopify` global the app calls. */
export const shopifyMock = {
  idToken: vi.fn(async () => 'session-token-1'),
  toast: { show: vi.fn() },
};

beforeEach(() => {
  shopifyMock.idToken.mockReset().mockResolvedValue('session-token-1');
  shopifyMock.toast.show.mockReset();
  globalThis.shopify = shopifyMock as unknown as typeof shopify;
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  window.history.replaceState(null, '', '/');
});
