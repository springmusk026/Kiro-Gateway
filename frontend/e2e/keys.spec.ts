import { admin, adminRequest, expect, projectValue, settled, test } from './fixtures'

// One gateway per project is shared by all files; these tests change its key
// list, so they run in order (the key created in the UI is on page 1 before
// the bulk keys arrive).
test.describe.configure({ mode: 'serial' })

const run = Date.now().toString(36)

// Start from an empty key list so the counts below are exact, whatever ran before.
test.beforeAll(async ({ playwright }, info) => {
  const api = await adminRequest(playwright, projectValue('URL', info.project.name))
  const { apiKeys } = (await (await api.get('/admin/api/api-keys')).json()) as { apiKeys: { id: string }[] | null }
  for (const k of apiKeys ?? []) expect((await api.delete(`/admin/api/api-keys/${k.id}`)).ok()).toBeTruthy()
  await api.dispose()
})

test('create an API key and see it in the list', async ({ page, admin: api }) => {
  await page.goto(admin('/keys'))
  await settled(page, 'API & Keys')

  const name = `e2e key ${run}`
  await page.getByRole('button', { name: 'Create key' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Create API key' })
  await dialog.getByLabel('Name').fill(name)
  await dialog.getByRole('button', { name: 'Create key' }).click()

  // The plaintext key is shown exactly once.
  const reveal = page.getByRole('dialog', { name: 'Your new API key' })
  await expect(reveal).toBeVisible()
  const plaintext = (await reveal.locator('code').textContent())?.trim() ?? ''
  expect(plaintext.length).toBeGreaterThanOrEqual(20)
  await reveal.getByRole('button', { name: /saved it|Done/ }).click()
  await expect(reveal).toBeHidden()

  await expect(page.getByText(name, { exact: true })).toBeVisible()
  // The list never shows the full key again.
  await expect(page.getByText(plaintext, { exact: true })).toHaveCount(0)

  const { apiKeys: keys } = (await (await api.get('/admin/api/api-keys')).json()) as { apiKeys: { name: string }[] }
  expect(keys.map((k) => k.name)).toContain(name)
})

test('pagination appears with more than 25 keys', async ({ page, admin: api }) => {
  const before = ((await (await api.get('/admin/api/api-keys')).json()) as { apiKeys: unknown[] }).apiKeys.length
  const add = 30
  for (let i = 0; i < add; i++) {
    const res = await api.post('/admin/api/api-keys', { data: { name: `bulk ${run} ${String(i).padStart(2, '0')}` } })
    expect(res.ok()).toBeTruthy()
  }
  const total = before + add

  await page.goto(admin('/keys'))
  await settled(page, 'API & Keys')
  const pager = page.getByRole('navigation', { name: 'Pagination' })
  await expect(pager).toBeVisible()
  await expect(page.getByText(`1–25 of ${total}`)).toBeVisible()
  await expect(pager.getByRole('button', { name: 'Previous page' })).toBeDisabled()

  await pager.getByRole('button', { name: 'Next page' }).click()
  await expect(page.getByText(`26–${Math.min(50, total)} of ${total}`)).toBeVisible()
  await expect(pager.getByRole('button', { name: '2', exact: true })).toHaveAttribute('aria-current', 'page')

  // A bigger page size goes back to page 1.
  await page.getByRole('combobox', { name: 'Rows per page' }).click()
  await page.getByRole('option', { name: '50' }).click()
  await expect(page.getByText(`1–${Math.min(50, total)} of ${total}`)).toBeVisible()
  await expect(pager.getByRole('button', { name: 'Next page' })).toBeEnabled({ enabled: total > 50 })
})
