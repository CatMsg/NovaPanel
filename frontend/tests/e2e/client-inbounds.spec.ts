import { expect, test } from '@playwright/test'
import { installBaseMocks, json } from './mockApi'

const inboundFixtures = [
  { id: 31, type: 'vless', tag: 'empty-vless', users: null },
  { id: 32, type: 'socks', tag: 'empty-socks', users: [] },
  { id: 33, type: 'dns', tag: 'non-user-protocol', users: [] },
  { id: 34, type: 'shadowsocks', tag: 'managed-shadowsocks', managed: true, users: [] },
  { id: 35, type: 'shadowtls', tag: 'shadowtls-v2', version: 2, users: [] },
  { id: '36', type: 'vless', tag: 'invalid-id', users: [] },
]

const pageData = {
  config: {},
  lastUpdate: 1,
  onlines: { inbound: [], outbound: [], user: [] },
  clients: [],
  inbounds: inboundFixtures,
  outbounds: [],
  endpoints: [],
  services: [],
  tls: [],
  enableTraffic: false,
}

test('new and bulk client forms offer supported empty-user inbounds only', async ({ page }) => {
  await installBaseMocks(page, true)
  let savedClient: Record<string, unknown> | undefined
  await page.route('**/api/load**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json(pageData),
  }))
  await page.route('**/api/preflight**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ changed: true, warnings: [] }),
  }))
  await page.route('**/api/save**', async route => {
    const postData = route.request().postDataJSON() as { data: string }
    savedClient = JSON.parse(postData.data)
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: json({ clients: [] }),
    })
  })

  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('/app/clients')
  await page.getByRole('button', { name: '添加', exact: true }).first().click()

  const clientDialog = page.getByRole('dialog').last()
  await clientDialog.getByRole('textbox', { name: '名称' }).fill('bound-to-empty-inbound')
  const clientInboundSelect = clientDialog.getByRole('combobox', { name: '入站标签' })
  await clientInboundSelect.press('ArrowDown')
  await expect(page.getByRole('option', { name: 'empty-vless' })).toBeVisible()
  await expect(page.getByRole('option', { name: 'empty-socks' })).toBeVisible()
  for (const tag of ['non-user-protocol', 'managed-shadowsocks', 'shadowtls-v2', 'invalid-id']) {
    await expect(page.getByRole('option', { name: tag })).toHaveCount(0)
  }
  await page.getByRole('option', { name: 'empty-vless' }).click()
  await clientDialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => savedClient?.inbounds).toEqual([31])
  expect(savedClient?.name).toBe('bound-to-empty-inbound')

  await page.getByRole('button', { name: '批量操作' }).click()
  await page.getByText('批量添加', { exact: true }).click()
  const bulkDialog = page.getByRole('dialog').last()
  await bulkDialog.getByRole('combobox', { name: '入站标签' }).press('ArrowDown')
  await expect(page.getByRole('option', { name: 'empty-vless' })).toBeVisible()
  await expect(page.getByRole('option', { name: 'empty-socks' })).toBeVisible()
  for (const tag of ['non-user-protocol', 'managed-shadowsocks', 'shadowtls-v2', 'invalid-id']) {
    await expect(page.getByRole('option', { name: tag })).toHaveCount(0)
  }
})
