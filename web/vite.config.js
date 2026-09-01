import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../internal/api/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      // insights-agent (chat). Override its origin in prod via VITE_AGENT_BASE_URL.
      '/ask': 'http://localhost:8099',
    },
  },
})
