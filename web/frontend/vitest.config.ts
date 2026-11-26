import { defineConfig, mergeConfig } from 'vitest/config';

import viteConfig from './vite.config.ts';

// Component and logic tests run in happy-dom with the app's own Vite
// config (Preact JSX, aliases). App Bridge's `shopify` global and fetch are
// stubbed per test in src/test/setup.ts; nothing talks to Shopify.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      environment: 'happy-dom',
      environmentOptions: { happyDOM: { url: 'https://app.example.com/' } },
      setupFiles: ['./src/test/setup.ts'],
      include: ['src/**/*.test.{ts,tsx}'],
      // TanStack Query imports `react`; inlining it sends that import
      // through Vite's react -> preact/compat alias, as in the app build.
      server: { deps: { inline: ['@tanstack/react-query'] } },
    },
  }),
);
