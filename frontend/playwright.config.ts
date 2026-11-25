import { defineConfig, devices } from '@playwright/test'

// End-to-end smoke tests against a real gateway binary serving the published
// console (../web, so run `npm run build` first). e2e/global-setup.ts builds and
// starts one gateway per project:
//   json      JSON config backend (always)
//   postgres  PostgreSQL backend, only when E2E_DATABASE_URL is set
// No test talks to Kiro: no accounts are added and the gateway's outbound
// proxy points at a closed local port, so any upstream call fails locally.

const projects = [{ name: 'json' }]
if (process.env.E2E_DATABASE_URL) projects.push({ name: 'postgres' })

const executablePath = process.env.PW_CHROMIUM || undefined

export default defineConfig({
  testDir: './e2e',
  globalSetup: './e2e/global-setup.ts',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: process.env.CI ? 2 : undefined,
  timeout: 30_000,
  expect: { timeout: 10_000 },
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : [['list']],
  use: {
    ...devices['Desktop Chrome'],
    viewport: { width: 1280, height: 800 },
    locale: 'en-US',
    colorScheme: 'light',
    trace: 'off',
    screenshot: 'only-on-failure',
    launchOptions: executablePath ? { executablePath } : {},
  },
  projects,
})
