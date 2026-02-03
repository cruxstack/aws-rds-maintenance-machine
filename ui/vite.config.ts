import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

// Backend server URL - defaults to demo server on port 8080
// Override with VITE_API_URL environment variable:
//   VITE_API_URL=http://localhost:3010 npm run dev  (for local server)
//   VITE_API_URL=http://localhost:8080 npm run dev  (for demo server)
const apiUrl = process.env.VITE_API_URL || 'http://localhost:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': apiUrl,
      '/mock': apiUrl,
    },
  },
})
