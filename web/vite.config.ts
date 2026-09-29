import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The dev server proxies to the Go API so the frontend can be developed with a
// single origin and without CORS preflights. In production VITE_API_BASE points
// at the Render service and the proxy is unused.
//
// The default has to be the port the Go service actually binds (config
// DefaultPort). It previously said 8099, which nothing listens on, so
// `npm run dev` produced a frontend whose every request failed to proxy and
// the only symptom was an empty map panel.
const target = process.env.ORCA_API ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target, changeOrigin: true },
      '/healthz': { target, changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
    rollupOptions: {
      output: {
        // Keep the map engine out of the critical path: the verdict and the trace
        // must render even if the map chunk is slow.
        manualChunks: {
          map: ['maplibre-gl'],
          react: ['react', 'react-dom'],
        },
      },
    },
  },
})
