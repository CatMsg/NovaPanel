import { expect, test, type Page } from '@playwright/test'
import { installBaseMocks, json } from './mockApi'

const dnsConfig = () => ({
  dns: {
    final: 'dns-main',
    servers: [
      { type: 'local', tag: 'dns-main' },
      { type: 'udp', tag: 'dns-unused', server: '192.0.2.53', server_port: 53 },
      { type: 'dhcp', tag: 'dns-map-only' },
    ],
    rules: [{
      action: 'route', domain: ['example.invalid'], server: 'dns-main',
      dns_server_address: { 'dns-main': '192.0.2.1', 'dns-map-only': ['192.0.2.10', '192.0.2.11'] },
      dns_search_domain: { 'dns-map-only': 'corp.example' },
    }],
  },
  route: {
    rules: [{
      action: 'resolve', server: 'dns-main',
      dns_search_domain: { 'dns-main': ['corp.example', 'internal.example'], 'dns-map-only': 'internal.example' },
    }],
    rule_set: [],
    default_domain_resolver: { server: 'dns-main', strategy: 'ipv4_only' },
  },
})

async function installDnsMocks(page: Page, externalReference = false) {
  await installBaseMocks(page)
  await page.route('**/api/load**', route => route.fulfill({
    status: 200, contentType: 'application/json',
    body: json({
      config: dnsConfig(), lastUpdate: 1,
      onlines: { inbound: [], outbound: [], user: [] },
      inbounds: [], clients: [], endpoints: [], services: [], tls: [],
      outbounds: externalReference
        ? [{ id: 1, type: 'direct', tag: 'proxy-main', domain_resolver: 'dns-main' }]
        : [],
    }),
  }))
  let saved: ReturnType<typeof dnsConfig> | undefined
  await page.route('**/api/preflight**', route => route.fulfill({
    status: 200, contentType: 'application/json', body: json({ changed: true, warnings: [] }),
  }))
  await page.route('**/api/save**', async route => {
    saved = JSON.parse(route.request().postDataJSON().data)
    await route.fulfill({ status: 200, contentType: 'application/json', body: json({ config: saved }) })
  })
  return () => saved
}

const card = (page: Page, tag: string) => page.locator('.np-resource-card').filter({ has: page.getByText(tag, { exact: true }) })

for (const width of [1280, 390]) {
  test(`DNS dependency confirmation blocks deletion at ${width}px`, async ({ page }) => {
    await installDnsMocks(page)
    await page.setViewportSize({ width, height: 850 })
    await page.goto('/app/dns')
    await card(page, 'dns-main').getByRole('button', { name: '删除', exact: true }).click()
    const dialog = page.getByRole('dialog').last()
    await expect(dialog.getByText('无法删除 DNS 服务器', { exact: true })).toBeVisible()
    await expect(dialog.getByText('默认 DNS', { exact: true })).toBeVisible()
    await expect(dialog.getByText('DNS 分流规则 #1', { exact: true })).toBeVisible()
    await expect(dialog.getByText('路由解析规则 #1', { exact: true })).toBeVisible()
    await expect(dialog.getByRole('button', { name: '确认', exact: true })).toBeDisabled()
    const bounds = await dialog.boundingBox()
    expect(bounds).not.toBeNull()
    expect(bounds!.x).toBeGreaterThanOrEqual(0)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width)
    await page.screenshot({ path: `test-results/dns-dependencies-${width}.png`, animations: 'disabled' })
    await dialog.getByRole('button', { name: '取消', exact: true }).click()
    await expect(card(page, 'dns-main')).toBeVisible()
  })
}

test('unused DNS deletion only changes the intended server and persists on save', async ({ page }) => {
  const saved = await installDnsMocks(page)
  await page.goto('/app/dns')
  await card(page, 'dns-unused').getByRole('button', { name: '删除', exact: true }).click()
  const dialog = page.getByRole('dialog').last()
  await expect(dialog.getByText('未发现引用，可以安全删除此服务器。')).toBeVisible()
  await dialog.getByRole('button', { name: '确认', exact: true }).click()
  await expect(card(page, 'dns-unused')).toHaveCount(0)
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => saved()?.dns.servers.map(server => server.tag)).toEqual(['dns-main', 'dns-map-only'])
  expect(saved()?.dns.rules).toEqual(dnsConfig().dns.rules)
})

test('DNS tag-map-only references block deleting a server', async ({ page }) => {
  await installDnsMocks(page)
  await page.goto('/app/dns')
  await card(page, 'dns-map-only').getByRole('button', { name: '删除', exact: true }).click()
  const dialog = page.getByRole('dialog').last()
  await expect(dialog.getByText('无法删除 DNS 服务器', { exact: true })).toBeVisible()
  await expect(dialog.getByText('DNS 分流规则 #1', { exact: true })).toBeVisible()
  await expect(dialog.getByText('路由解析规则 #1', { exact: true })).toBeVisible()
  await expect(dialog.getByRole('button', { name: '确认', exact: true })).toBeDisabled()
})

test('DNS rename migrates config references while retaining resolver options', async ({ page }) => {
  const saved = await installDnsMocks(page)
  await page.goto('/app/dns')
  await card(page, 'dns-main').getByRole('button', { name: '编辑', exact: true }).click()
  const editor = page.getByRole('dialog').last()
  await editor.getByRole('textbox', { name: '标签', exact: true }).fill('dns-renamed')
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await expect(editor).toBeHidden()
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => saved()?.dns.final).toBe('dns-renamed')
  expect(saved()?.dns.rules[0].server).toBe('dns-renamed')
  expect(saved()?.dns.rules[0].dns_server_address).toEqual({ 'dns-renamed': '192.0.2.1', 'dns-map-only': ['192.0.2.10', '192.0.2.11'] })
  expect(saved()?.route.rules[0].server).toBe('dns-renamed')
  expect(saved()?.route.rules[0].dns_search_domain).toEqual({ 'dns-renamed': ['corp.example', 'internal.example'], 'dns-map-only': 'internal.example' })
  expect(saved()?.route.default_domain_resolver).toEqual({ server: 'dns-renamed', strategy: 'ipv4_only' })
})

test('DNS editor refuses blank, duplicate and externally referenced tag changes', async ({ page }) => {
  await installDnsMocks(page, true)
  await page.goto('/app/dns')
  await card(page, 'dns-main').getByRole('button', { name: '编辑', exact: true }).click()
  const editor = page.getByRole('dialog').last()
  const tag = editor.getByRole('textbox', { name: '标签', exact: true })
  await tag.fill(' ')
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByText('DNS 标签不能为空。', { exact: true })).toBeVisible()
  await tag.fill('dns-unused')
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByText('DNS 标签重复', { exact: true })).toBeVisible()
  await tag.fill('dns-renamed')
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByText('无法重命名 DNS 服务器', { exact: true })).toBeVisible()
  await expect(page.getByText(/proxy-main.*domain_resolver/)).toBeVisible()
  await expect(editor).toBeVisible()
  await editor.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(card(page, 'dns-main')).toBeVisible()
  await expect(card(page, 'dns-renamed')).toHaveCount(0)
})
