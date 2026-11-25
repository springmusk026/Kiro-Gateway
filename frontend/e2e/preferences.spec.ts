import { admin, expect, settled, test } from './fixtures'

test.beforeEach(async ({ page }) => {
  await page.goto(admin('/'))
  await settled(page, 'Overview')
})

test('language switch changes the navigation labels and is remembered', async ({ page }) => {
  const nav = page.getByRole('complementary')
  await expect(nav.getByRole('link', { name: 'Overview', exact: true })).toBeVisible()

  const choose = async (label: string) => {
    await page.getByRole('button', { name: /^(Language|语言|Ngôn ngữ)$/ }).click()
    await page.getByRole('menuitemradio', { name: label }).click()
  }

  await choose('中文')
  await expect(nav.getByRole('link', { name: '概览', exact: true })).toBeVisible()
  await expect(nav.getByRole('link', { name: '设置', exact: true })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh')

  await choose('Tiếng Việt')
  await expect(nav.getByRole('link', { name: 'Tổng quan', exact: true })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'vi')

  await page.reload()
  await expect(nav.getByRole('link', { name: 'Tổng quan', exact: true })).toBeVisible()

  await choose('English')
  await expect(nav.getByRole('link', { name: 'Overview', exact: true })).toBeVisible()
})

test('dark mode toggle applies and persists the theme', async ({ page }) => {
  const html = page.locator('html')
  await expect(html).not.toHaveClass(/\bdark\b/)

  const choose = async (label: string) => {
    await page.getByRole('button', { name: 'Theme' }).click()
    await page.getByRole('menuitemradio', { name: label }).click()
  }

  await choose('Dark')
  await expect(html).toHaveClass(/\bdark\b/)
  const bg = await page.evaluate(() => getComputedStyle(document.body).backgroundColor)

  await page.reload()
  await expect(html).toHaveClass(/\bdark\b/)

  await choose('Light')
  await expect(html).not.toHaveClass(/\bdark\b/)
  expect(await page.evaluate(() => getComputedStyle(document.body).backgroundColor)).not.toBe(bg)
})
