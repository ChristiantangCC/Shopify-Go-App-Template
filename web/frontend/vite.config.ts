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
  },
});
