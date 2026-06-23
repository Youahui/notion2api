import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:9998',
        changeOrigin: true
      },
      '/admin': {
        target: 'http://127.0.0.1:9998',
        changeOrigin: true,
        bypass(req) {
          if (req.headers.accept?.includes('text/html')) {
            return '/index.html';
          }
          return undefined;
        }
      },
      '/healthz': 'http://127.0.0.1:9998'
    }
  }
});
