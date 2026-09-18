import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': { target: process.env.DEV_API_PROXY_TARGET || 'http://localhost:15681', changeOrigin: true },
    },
    watch: { usePolling: process.env.DEV_WATCH_POLL === 'true', interval: 500 },
    fs: {
      allow: [fileURLToPath(new URL('.', import.meta.url)), fileURLToPath(new URL('../shared', import.meta.url))],
    },
  },
});
