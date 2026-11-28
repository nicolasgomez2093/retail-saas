import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { resolve } from 'node:path';

// The public storefront is its own page (tienda.html) so shoppers do not download the back office.
// In development /tienda/<comercio>/... serves that page, as Caddy does in production.
const storefrontPages = {
  name: 'storefront-pages',
  configureServer(server) {
    server.middlewares.use((req, _res, next) => {
      if (req.url && /^\/tienda(\/|$)/.test(req.url)) req.url = '/tienda.html';
      next();
    });
  },
};

export default defineConfig({
  plugins: [react(), storefrontPages],
  server: { proxy: { '/api': process.env.API_PROXY || 'http://localhost:8080' } },
  build: {
    rollupOptions: {
      input: { main: resolve(import.meta.dirname, 'index.html'), tienda: resolve(import.meta.dirname, 'tienda.html') },
    },
  },
});
