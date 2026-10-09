import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'
import { writeFileSync } from 'node:fs'

export default defineConfig({
  plugins: [react(), tailwindcss(), { name: 'embed-placeholder', closeBundle: () => writeFileSync(fileURLToPath(new URL('../../internal/web/dist/.keep', import.meta.url)), '') }],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  build: { outDir: '../../internal/web/dist', emptyOutDir: true, sourcemap: false },
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: false },
      '/healthz': { target: 'http://127.0.0.1:8080', changeOrigin: false },
    },
  },
  test: { include: ['src/**/*.test.{ts,tsx}'], environment: 'jsdom', setupFiles: ['./src/test/setup.ts'], restoreMocks: true },
})
