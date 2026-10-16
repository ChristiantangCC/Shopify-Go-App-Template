import preact from '@preact/preset-vite';
import { defineConfig } from 'vite';

// The Go server proxies to this dev server when FRONTEND_DEV_URL is set;
// @preact/preset-vite aliases react and react-dom to preact/compat, which
// is what lets TanStack Query run on Preact.
export default defineConfig({
  plugins: [preact()],
  server: {
    port: 5173,
    strictPort: true,
    // The page loads through the dev tunnel, but the HMR websocket goes
    // straight to localhost: tunnels drop idle websockets, and every drop
    // makes the Vite client force a full page reload. Browsers allow
    // ws://localhost from https pages, so this is safe.
    hmr: {
      protocol: 'ws',
      host: 'localhost',
      port: 5173,
    },
  },
});
