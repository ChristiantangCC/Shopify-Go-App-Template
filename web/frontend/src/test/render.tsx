import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/preact';
import type { ComponentChildren } from 'preact';

/** Renders with a fresh query client that never retries, so errors show at once. */
export const renderWithQuery = (ui: ComponentChildren) => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
};
