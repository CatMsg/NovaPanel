import { expect, test } from '@playwright/test'
import { installBaseMocks, json } from './mockApi'

const pageData = {
  config: {},
  lastUpdate: 1,
  onlines: { inbound: [], outbound: [], user: [] },
  inbounds: [{ id: 1, type: 'vless', tag: 'vless-in', listen: '0.0.0.0', listen_port: 443, tls_id: 0, transport: {} }],
  outbounds: [{ id: 1, type: 'socks', tag: 'jp-proxy', server: 'jp.example', server_port: 1080 }],
  clients: [],
  endpoints: [],
  services: [],
  tls: [],
  enableTraffic: false,
}

test('inbound and outbound lists search on desktop and remain usable on mobile', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/load**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json(pageData),
  }))

  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('/app/inbounds')
  await expect(page.locator('.resource-table')).toBeVisible()
  await expect(page.getByText('vless-in', { exact: true })).toBeVisible()
  await page.getByPlaceholder('搜索标签、协议、地址、端口或用户').fill('no-such-inbound')
  await expect(page.getByRole('heading', { name: '没有匹配项' })).toBeVisible()

  await page.goto('/app/outbounds')
  const outboundTable = page.locator('.resource-table')
  await expect(outboundTable).toBeVisible()
  await expect(outboundTable.getByText('jp-proxy', { exact: true })).toBeVisible()
  await page.getByPlaceholder('搜索标签、类型、服务器或端口').fill('no-such-outbound')
  await expect(page.getByRole('heading', { name: '没有匹配项' })).toBeVisible()

  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/app/inbounds')
  const inboundCard = page.locator('.resource-card').filter({ hasText: 'vless-in' })
  await expect(inboundCard).toBeVisible()
  await inboundCard.getByRole('button', { name: '删除' }).click()

  const confirmation = page.getByRole('dialog')
  await expect(confirmation).toBeVisible()
  await expect(confirmation.getByText('删除 · vless-in')).toBeVisible()
  const bounds = await confirmation.boundingBox()
  expect(bounds).not.toBeNull()
  expect(bounds!.width).toBeLessThanOrEqual(390)
  await confirmation.getByRole('button', { name: '取消' }).click()
  await expect(confirmation).toBeHidden()

  await page.route('**/api/inbounds**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ inbounds: pageData.inbounds.map(item => ({ ...item, addrs: [], out_json: {} })) }),
  }))
  await inboundCard.getByRole('button', { name: '编辑' }).click()
  const editor = page.getByRole('dialog').last()
  await expect(editor.getByText('高级传输设置', { exact: true })).toBeVisible()
  await expect(editor.getByText('传输', { exact: true })).toBeHidden()
  const editorBounds = await editor.boundingBox()
  expect(editorBounds).not.toBeNull()
  expect(editorBounds!.width).toBeLessThanOrEqual(390)
  await editor.getByText('高级传输设置', { exact: true }).click()
  await expect(editor.getByText('传输', { exact: true })).toBeVisible()

  await page.goto('/app/outbounds')
  await expect(page.locator('.resource-card').filter({ hasText: 'jp-proxy' })).toBeVisible()
})
