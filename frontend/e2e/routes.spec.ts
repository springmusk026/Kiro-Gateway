import { admin, expect, settled, test, type Page } from './fixtures'

/** Every console page, with its label in the header. */
const ROUTES = [
  { path: '/', heading: 'Overview' },
  { path: '/analytics', heading: 'Analytics' },
  { path: '/accounts', heading: 'Accounts' },
  { path: '/models', heading: 'Models' },
  { path: '/models?tab=aliases', heading: 'Models' },
  { path: '/models?tab=thinking', heading: 'Models' },
  { path: '/playground', heading: 'Playground' },
  { path: '/keys', heading: 'API & Keys' },
  { path: '/logs', heading: 'Request logs' },
  { path: '/settings', heading: 'Settings' },
]

async function open(page: Page, path: string, heading: string) {
  await page.goto(admin(path))
  await settled(page, heading)
}

test.describe('every page renders without console errors', () => {
  for (const r of ROUTES) {
    test(r.path, async ({ page }) => {
      await open(page, r.path, r.heading)
    })
  }

  test('Models tabs switch in place', async ({ page }) => {
    await open(page, '/models', 'Models')
    for (const [tab, query] of [
      [/aliases/i, 'tab=aliases'],
      [/thinking/i, 'tab=thinking'],
    ] as const) {
      await page.getByRole('tab', { name: tab }).click()
      await expect(page).toHaveURL(new RegExp(query))
      await expect(page.getByRole('tab', { name: tab })).toHaveAttribute('aria-selected', 'true')
    }
  })

  test('the sidebar links reach every page', async ({ page }) => {
    await open(page, '/', 'Overview')
    const nav = page.getByRole('complementary')
    for (const r of ROUTES.filter((r) => !r.path.includes('?'))) {
      await nav.getByRole('link', { name: r.heading, exact: true }).click()
      await settled(page, r.heading)
    }
  })
})

test.describe('phone width (390×844)', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })

  for (const r of ROUTES) {
    test(`no horizontal overflow on ${r.path}`, async ({ page }) => {
      await open(page, r.path, r.heading)
      const { scroll, client, wide } = await page.evaluate(() => {
        const vw = document.documentElement.clientWidth
        // Name the widest offenders to make a failure actionable.
        const wide = [...document.querySelectorAll<HTMLElement>('body *')]
          .filter((el) => el.getBoundingClientRect().right > vw + 1)
          .filter((el) => !el.closest('[data-overflow-ok], .overflow-x-auto, .overflow-auto, .overflow-x-scroll'))
          .slice(0, 5)
          .map((el) => `${el.tagName.toLowerCase()}.${String(el.className).split(' ').slice(0, 4).join('.')}`)
        return { scroll: document.documentElement.scrollWidth, client: vw, wide }
      })
      expect(scroll, `page is ${scroll}px wide in a ${client}px viewport; overflowing: ${wide.join(', ')}`).toBeLessThanOrEqual(client)
    })
  }
})
