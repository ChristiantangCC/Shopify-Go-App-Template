import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render } from 'preact';

import { App } from './app';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
    },
  },
});

render(
  <QueryClientProvider client={queryClient}>
    <App />
  </QueryClientProvider>,
  document.getElementById('app')!,
);
