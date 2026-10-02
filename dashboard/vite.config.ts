import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The dashboard talks to two Atlas listeners: the public API (:8080) and the
// admin API (:8082, no CORS by design). Same-origin relative paths plus this
// path split keep the browser view single-origin.
const proxy = {
  '/v1/admin': { target: 'http://localhost:8082', changeOrigin: true },
  '/v1': { target: 'http://localhost:8080', changeOrigin: true },
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: { proxy },
  preview: { proxy },
})
