import { defineConfig } from 'vitest/config'
import viteReact from '@vitejs/plugin-react'

// Unit tests run in jsdom without the TanStack Start plugin: the modules under
// test are plain React and only need the `@/` alias. End-to-end tests live in
// e2e/ and run with Playwright (playwright.config.ts).
export default defineConfig({
  plugins: [viteReact()],
  resolve: { tsconfigPaths: true },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.{ts,tsx}'],
    setupFiles: ['src/test/setup.ts'],
    restoreMocks: true,
    unstubGlobals: true,
  },
})
