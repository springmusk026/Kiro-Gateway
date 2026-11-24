import { defineConfig } from 'vite'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import viteReact from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// The console is a static SPA served by the Go binary under /admin.
// During development, API calls are proxied to a local Kiro-Go instance.
export default defineConfig({
  base: '/admin/',
  resolve: { tsconfigPaths: true },
  server: {
    proxy: {
      '/admin/api': { target: process.env.KIRO_GO_URL ?? 'http://127.0.0.1:8080', changeOrigin: true },
      '/v1': { target: process.env.KIRO_GO_URL ?? 'http://127.0.0.1:8080', changeOrigin: true },
    },
  },
  plugins: [
    tailwindcss(),
    tanstackStart({
      router: { basepath: '/admin' },
      spa: { enabled: true },
    }),
    viteReact(),
  ],
})
