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

test('TLS cards show explicit enabled states instead of yes/no action labels', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/load**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({
      ...pageData,
      inbounds: [{ ...pageData.inbounds[0], tls_id: 1 }],
      tls: [{
        id: 1,
        name: 'VLESS-Reality',
        server: { server_name: 'gateway.icloud.com', ech: { enabled: false }, reality: { enabled: true } },
        client: {},
      }],
    }),
  }))

  await page.goto('/app/tls')

  const card = page.locator('.resource-card').filter({ hasText: 'VLESS-Reality' })
  await expect(card).toBeVisible()
  await expect(card.getByText('已启用', { exact: true })).toBeVisible()
  await expect(card.getByText('未启用', { exact: true })).toHaveCount(2)
  await expect(card.getByText('确认', { exact: true })).toHaveCount(0)
  await expect(card.getByText('取消', { exact: true })).toHaveCount(0)
})

test('editing a legacy ACME TLS profile saves certificate_provider format', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/load**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({
      ...pageData,
      inbounds: [{ ...pageData.inbounds[0], tls_id: 1 }],
      tls: [{
        id: 1,
        name: 'legacy-acme',
        server: {
          enabled: true,
          certificate_path: '/etc/novas/cert.pem',
          key_path: '/etc/novas/key.pem',
          acme: { domain: 'edge.example', email: 'ops@example.com' },
        },
        client: {},
      }],
    }),
  }))

  let savedTLS: { server: Record<string, unknown> } | undefined
  await page.route('**/api/preflight', async route => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: json({ changed: true }) })
  })
  await page.route('**/api/save', async route => {
    const request = route.request().postDataJSON()
    savedTLS = JSON.parse(request.data)
    await route.fulfill({ status: 200, contentType: 'application/json', body: json({}) })
  })

  await page.goto('/app/tls')
  const card = page.locator('.resource-card').filter({ hasText: 'legacy-acme' })
  await card.getByRole('button', { name: '编辑' }).click()
  const editor = page.getByRole('dialog').last()
  await expect(editor.getByRole('textbox', { name: '域名 （逗号分隔）' })).toHaveValue('edge.example')
  await editor.getByRole('button', { name: '保存', exact: true }).click()

  await expect.poll(() => savedTLS).toBeDefined()
  expect(savedTLS?.server).not.toHaveProperty('acme')
  expect(savedTLS?.server.certificate_provider).toEqual({
    type: 'acme',
    domain: ['edge.example'],
    email: 'ops@example.com',
  })
})

test('modern ACME provider accepts sing-box listable scalar domain in the editor', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/load**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({
      ...pageData,
      tls: [{
        id: 1,
        name: 'modern-acme',
        server: {
          enabled: true,
          certificate_provider: { type: 'acme', domain: 'edge.example', email: 'ops@example.com' },
        },
        client: {},
      }],
    }),
  }))

  await page.goto('/app/tls')
  const card = page.locator('.resource-card').filter({ hasText: 'modern-acme' })
  await card.getByRole('button', { name: '编辑' }).click()
  const editor = page.getByRole('dialog').last()
  await expect(editor.getByRole('textbox', { name: '域名 （逗号分隔）' })).toHaveValue('edge.example')
  await expect(editor.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
})
