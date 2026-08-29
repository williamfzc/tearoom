import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The API is same-origin in dev (proxied to the local hosted server). The
// production build points VITE_API_BASE at the Render service and is served
// from Cloudflare Pages; the server's --cors-origin allows that origin.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8787',
      '/r': 'http://localhost:8787',
    },
  },
})
