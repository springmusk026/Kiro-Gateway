import { ADMIN_PASSWORD } from './env'
import { admin, expect, settled, test } from './fixtures'

// Signed out, the session check and a wrong password are answered with 401s,
// which Chromium logs as failed resources.
test.use({ signedIn: false, allowConsoleErrors: [/status of 401/] })

test.describe('sign-in', () => {
  test('redirects to the login page without a session', async ({ page }) => {
    await page.goto(admin('/keys'))
    await expect(page).toHaveURL(/\/admin\/login$/)
    await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible()
  })

  test('wrong password stays on the login page; the right one lands on Overview with an HttpOnly session', async ({ page, context }) => {
    await page.goto(admin('/login'))
    const pw = page.getByLabel('Admin password')
    const submit = page.getByRole('button', { name: 'Sign in' })
    await expect(submit).toBeDisabled()

    await pw.fill('not-the-password')
    await submit.click()
    await expect(page.getByText('That password is not correct')).toBeVisible()
    await expect(page).toHaveURL(/\/admin\/login$/)
    expect(await context.cookies()).toEqual([])

    await pw.fill(ADMIN_PASSWORD)
    await submit.click()
    await expect(page).toHaveURL(/\/admin\/?$/)
    await settled(page, 'Overview')

    const cookies = await context.cookies()
    expect(cookies).toHaveLength(1)
    expect(cookies[0]).toMatchObject({ path: '/admin', httpOnly: true, sameSite: 'Strict' })
    // Never readable from page scripts, and the password is not kept in web storage.
    expect(await page.evaluate(() => document.cookie)).toBe('')
    const stored = await page.evaluate(() => JSON.stringify({ ...localStorage }) + JSON.stringify({ ...sessionStorage }))
    expect(stored).not.toContain(ADMIN_PASSWORD)
  })
})

test('sign out ends the session', async ({ page, context }) => {
  await page.goto(admin('/login'))
  await page.getByLabel('Admin password').fill(ADMIN_PASSWORD)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await settled(page)
  const session = await context.cookies()
  expect(session).toHaveLength(1)

  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/admin\/login$/)
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible()

  await page.goto(admin('/'))
  await expect(page).toHaveURL(/\/admin\/login$/)

  // The server forgot the session: a copy of the old cookie is useless.
  await context.addCookies(session)
  const res = await context.request.get('/admin/api/status', { headers: { 'X-Requested-With': 'e2e' } })
  expect(res.status()).toBe(401)
})
