import { test as base, expect, type APIRequestContext, type Page, type PlaywrightWorkerArgs } from '@playwright/test'
import { ADMIN_PASSWORD, envKey } from './env'

export { expect, type Page }

type Options = {
  /** Start signed in (the session saved by global setup). */
  signedIn: boolean
  /** Console errors that a test expects (e.g. the 401 of a wrong password). */
  allowConsoleErrors: RegExp[]
}

type Fixtures = {
  /** Admin API client authenticated with the password header. */
  admin: APIRequestContext
  /** Console errors and uncaught exceptions seen during the test; the test fails if any are left. */
  pageErrors: string[]
}

export function projectValue(what: 'URL' | 'STATE', project: string): string {
  const v = process.env[envKey(what, project)]
  if (!v) throw new Error(`${envKey(what, project)} is not set: global setup did not start a gateway for project "${project}"`)
  return v
}

/** Admin API client for a project's gateway, authenticated with the password header. */
export function adminRequest(playwright: PlaywrightWorkerArgs['playwright'], baseURL: string): Promise<APIRequestContext> {
  return playwright.request.newContext({
    baseURL,
    extraHTTPHeaders: { 'X-Admin-Password': ADMIN_PASSWORD, 'X-Requested-With': 'e2e' },
  })
}

export const test = base.extend<Options & Fixtures>({
  signedIn: [true, { option: true }],
  allowConsoleErrors: [[], { option: true }],

  baseURL: async ({}, use, info) => {
    await use(projectValue('URL', info.project.name))
  },

  storageState: async ({ signedIn }, use, info) => {
    await use(signedIn ? projectValue('STATE', info.project.name) : { cookies: [], origins: [] })
  },

  admin: async ({ playwright, baseURL }, use) => {
    const ctx = await adminRequest(playwright, baseURL!)
    await use(ctx)
    await ctx.dispose()
  },

  pageErrors: [
    async ({ page, allowConsoleErrors }, use, info) => {
      const errors: string[] = []
      page.on('console', (msg) => {
        if (msg.type() !== 'error') return
        const text = msg.text()
        if (allowConsoleErrors.some((re) => re.test(text))) return
        // Without a database, history endpoints answer 409 by design and the
        // console shows its "needs PostgreSQL" state instead.
        if (info.project.name === 'json' && /status of 409/.test(text) && msg.location().url.includes('/admin/api/')) return
        errors.push(`console.error: ${text} (${msg.location().url})`)
      })
      page.on('pageerror', (err) => errors.push(`uncaught: ${err.message}`))
      await use(errors)
      expect(errors, 'console errors or uncaught exceptions').toEqual([])
    },
    { auto: true },
  ],
})

/** Path inside the console. */
export const admin = (path = '/') => '/admin' + (path === '/' ? '/' : path)

/** Waits until the console shell shows `page` as the current page and nothing is loading. */
export async function settled(page: Page, label?: string) {
  if (label) await expect(page.getByRole('banner').getByText(label, { exact: true })).toBeVisible()
  await expect(page.getByRole('main')).not.toBeEmpty()
  await expect(page.locator('main [data-slot="skeleton"]')).toHaveCount(0)
}
