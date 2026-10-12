import { expect, test, type Locator, type Page } from '@playwright/test'
import { installBaseMocks, json } from './mockApi'

interface HttpFixtures {
  inbounds?: Record<string, unknown>[]
  outbounds?: Record<string, unknown>[]
  tls?: Record<string, unknown>[]
  config?: Record<string, unknown>
}

async function openSelect(select: Locator) {
  const input = select.last()
  await input.focus()
  await input.press('ArrowDown')
}

async function installHttpMocks(page: Page, fixtures: HttpFixtures = {}) {
  const inbounds = fixtures.inbounds ?? []
  const outbounds = fixtures.outbounds ?? []
  let preflightPayloads: Array<Record<string, unknown>> = []
  let savedPayloads: Array<Record<string, unknown>> = []

  await installBaseMocks(page)
  await page.route('**/api/load**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({
      config: fixtures.config ?? { dns: { servers: [], rules: [] }, route: { rules: [], rule_set: [] } },
      lastUpdate: 1,
      onlines: { inbound: [], outbound: [], user: [] },
      inbounds,
      outbounds,
      clients: [],
      endpoints: [],
      services: [],
      tls: fixtures.tls ?? [],
    }),
  }))
  await page.route('**/api/inbounds**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ inbounds }),
  }))
  await page.route('**/api/preflight**', async route => {
    preflightPayloads.push(route.request().postDataJSON())
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: json({ changed: true, warnings: [] }),
    })
  })
  await page.route('**/api/save**', async route => {
    const payload = route.request().postDataJSON()
    const data = typeof payload.data === 'string' ? JSON.parse(payload.data) : payload.data
    savedPayloads.push(data)
    await route.fulfill({ status: 200, contentType: 'application/json', body: json({}) })
  })
  await page.route('**/api/ruleset-health**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json([]),
  }))

  return {
    preflightPayloads: () => preflightPayloads,
    savedPayloads: () => savedPayloads,
  }
}

test('new HTTP inbound blocks invalid TLS and client versions before save', async ({ page }) => {
  const api = await installHttpMocks(page, {
    tls: [
      { id: 7, name: 'Disabled TLS', server: { enabled: false } },
      { id: 8, name: 'Active TLS', server: { enabled: true } },
    ],
  })
  await page.goto('/app/inbounds')
  await page.getByRole('button', { name: '添加', exact: true }).click()

  const editor = page.getByRole('dialog').last()
  await openSelect(editor.getByRole('combobox', { name: '类型' }))
  await page.getByRole('option', { name: 'HTTP', exact: true }).click()
  const save = editor.getByRole('button', { name: '保存', exact: true })
  const versions = editor.getByRole('combobox', { name: '提供的 HTTP 版本' })
  await expect(versions).toHaveValue(/HTTP\/1.*HTTP\/2/)

  await openSelect(versions)
  await page.getByRole('option', { name: 'HTTP/1', exact: true }).click()
  await expect(editor.getByText(/仅提供 HTTP\/2 的入站需要启用 TLS/)).toBeVisible()
  await expect(save).toBeDisabled()
  await versions.press('Escape')
  await openSelect(versions)
  await page.getByRole('option', { name: 'HTTP/1', exact: true }).click()

  await openSelect(versions)
  await page.getByRole('option', { name: 'HTTP/3', exact: true }).click()
  await expect(editor.getByText(/HTTP\/3 要求启用 TLS/).first()).toBeVisible()
  await expect(save).toBeDisabled()
  await save.click({ force: true })
  expect(api.preflightPayloads()).toHaveLength(0)

  await openSelect(editor.getByRole('combobox', { name: '模板' }))
  await page.getByRole('option', { name: 'Disabled TLS', exact: true }).click()
  await expect(save).toBeDisabled()
  await openSelect(editor.getByRole('combobox', { name: '模板' }))
  await page.getByRole('option', { name: 'Active TLS', exact: true }).click()
  await expect(save).toBeEnabled()

  await editor.getByRole('tab', { name: '服务器端' }).click()
  await openSelect(versions)
  await page.getByRole('option', { name: 'HTTP/3', exact: true }).click()
  await editor.getByRole('tab', { name: '客户端' }).click()
  await editor.getByRole('button', { name: 'HTTP 客户端高级选项', exact: true }).click()
  const clientVersion = editor.getByRole('combobox', { name: 'HTTP 版本' }).last()
  await openSelect(clientVersion)
  await page.getByRole('option', { name: 'HTTP/3', exact: true }).click()
  await expect(editor.getByText(/显式客户端 HTTP 版本未由此入站提供/).first()).toBeVisible()
  await expect(save).toBeDisabled()

  await openSelect(clientVersion)
  await page.getByRole('option', { name: '自动', exact: true }).click()
  await expect(save).toBeEnabled()
  await save.click()
  await expect.poll(() => api.savedPayloads()).toHaveLength(1)
  const savedInbound = api.savedPayloads()[0] as Record<string, any>
  expect(savedInbound.type).toBe('http')
  expect([...savedInbound.version].sort()).toEqual([1, 2])
  expect(savedInbound.tls_id).toBe(8)
  expect(savedInbound.out_json?.version).toBeUndefined()
})

test('imported scalar HTTP inbound version displays and round-trips unchanged', async ({ page }) => {
  const inbound = {
    id: 41,
    type: 'http',
    tag: 'scalar-http',
    listen: '127.0.0.1',
    listen_port: 8080,
    tls_id: 0,
    version: 1,
    out_json: { version: 1 },
    addrs: [],
  }
  const api = await installHttpMocks(page, { inbounds: [inbound] })
  await page.goto('/app/inbounds')
  const inboundRow = page.getByRole('row', { name: /scalar-http/ })
  await inboundRow.getByRole('button', { name: '编辑' }).click()
  const editor = page.getByRole('dialog').last()
  await expect(editor.getByText('HTTP/1', { exact: true })).toBeVisible()
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => api.savedPayloads()).toHaveLength(1)
  expect((api.savedPayloads()[0] as Record<string, any>).version).toBe(1)
})

test('HTTP outbound blocks H2 without TLS when fallback is disabled and H3 without TLS', async ({ page }) => {
  const api = await installHttpMocks(page, {
    outbounds: [{
      id: 51,
      type: 'http',
      tag: 'http-proxy',
      server: 'proxy.example',
      server_port: 8080,
      version: 0,
      tls: { enabled: false },
    }],
  })
  await page.goto('/app/outbounds')
  const outboundRow = page.getByRole('row', { name: /http-proxy/ })
  await outboundRow.getByRole('button', { name: '编辑' }).click()
  const editor = page.getByRole('dialog').last()
  await editor.getByRole('button', { name: 'HTTP 高级选项', exact: true }).click()
  const version = editor.getByRole('combobox', { name: 'HTTP 版本' })
  await openSelect(version)
  await page.getByRole('option', { name: 'HTTP/2', exact: true }).click()
  const fallback = editor.getByRole('checkbox', { name: '禁用版本回退' })
  await fallback.click()
  const save = editor.getByRole('button', { name: '保存', exact: true })
  await expect(editor.getByText(/禁用版本回退时，HTTP\/2 需要启用 TLS/).first()).toBeVisible()
  await expect(save).toBeDisabled()
  await save.click({ force: true })
  expect(api.preflightPayloads()).toHaveLength(0)

  await openSelect(version)
  await page.getByRole('option', { name: 'HTTP/3', exact: true }).click()
  await expect(save).toBeDisabled()
  await editor.getByRole('checkbox', { name: '启用 TLS' }).click()
  await expect(save).toBeEnabled()
  await save.click()
  await expect.poll(() => api.savedPayloads()).toHaveLength(1)
  const savedOutbound = api.savedPayloads()[0] as Record<string, any>
  expect(savedOutbound.version).toBe(3)
  expect(savedOutbound.disable_version_fallback).toBe(true)
  expect(savedOutbound.tls.enabled).toBe(true)
})

test('DNS tag conditions edit and save imported maps without losing unrelated data', async ({ page }) => {
  const config = {
    dns: {
      final: 'dns-local',
      servers: [
        { type: 'local', tag: 'dns-local' },
        { type: 'dhcp', tag: 'dns-dhcp' },
        { type: 'udp', tag: 'dns-udp', server: '192.0.2.53', server_port: 53 },
      ],
      rules: [{
        action: 'route',
        domain: ['example.test'],
        query_type: ['A'],
        server: 'dns-local',
        dns_server_address: {
          'dns-local': '192.0.2.1',
          'dns-orphan': '203.0.113.0/24',
          'dns-udp': '198.51.100.53',
        },
        dns_search_domain: { 'dns-local': ['corp.example', 'internal.example'] },
      }],
    },
    route: {
      rules: [{
        action: 'route',
        outbound: 'direct',
        dns_server_address: { 'dns-local': '192.0.2.1' },
        dns_search_domain: { 'dns-local': ['corp.example', 'internal.example'], 'dns-orphan': 'orphan.example' },
      }],
      rule_set: [],
    },
  }
  const api = await installHttpMocks(page, { config })
  await page.goto('/app/rules')
  const dnsRuleCard = page.locator('.np-resource-card').filter({ hasText: 'example.test' })
  await dnsRuleCard.getByRole('button', { name: '编辑', exact: true }).click()
  const editor = page.getByRole('dialog').last()
  await expect(editor.getByText('编辑 DNS规则', { exact: true })).toBeVisible()
  await editor.getByRole('button', { name: 'DNS 高级条件', exact: true }).click()
  await expect(editor.getByText(/此导入条件引用了未知 DNS 标签 dns-orphan/)).toBeVisible()
  await expect(editor.getByText(/DNS 服务器 dns-udp 使用的传输类型 udp 不受此条件支持/)).toBeVisible()

  const serverPicker = editor.getByRole('combobox', { name: '选择受支持的 DNS 服务器' }).first()
  await openSelect(serverPicker)
  await expect(page.getByRole('option', { name: 'dns-dhcp', exact: true })).toBeVisible()
  await expect(page.getByRole('option', { name: 'dns-udp', exact: true })).toHaveCount(0)
  await page.getByRole('option', { name: 'dns-dhcp', exact: true }).click()
  await editor.getByRole('textbox', { name: 'IP 地址或 CIDR' }).last().fill('198.51.100.1\n198.51.100.2')
  await editor.getByRole('button', { name: '添加条件', exact: true }).first().click()
  await editor.getByRole('button', { name: '移除条件 dns-udp', exact: true }).click()
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await expect(editor).toBeHidden()

  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => api.savedPayloads()).toHaveLength(1)
  const savedConfig = api.savedPayloads()[0] as Record<string, any>
  expect(savedConfig.dns.rules[0]).toMatchObject({
    action: 'route',
    domain: ['example.test'],
    query_type: ['A'],
    server: 'dns-local',
  })
  expect(savedConfig.dns.rules[0].dns_server_address).toEqual({
    'dns-local': '192.0.2.1',
    'dns-orphan': '203.0.113.0/24',
    'dns-dhcp': ['198.51.100.1', '198.51.100.2'],
  })
  expect(savedConfig.dns.rules[0].dns_search_domain).toEqual({
    'dns-local': ['corp.example', 'internal.example'],
  })
  expect(savedConfig.route.rules[0].dns_search_domain).toEqual({
    'dns-local': ['corp.example', 'internal.example'],
    'dns-orphan': 'orphan.example',
  })
})

test('implicit local DNS tag remains valid only when no servers or explicit final are configured', async ({ page }) => {
  const config = {
    dns: {
      servers: [],
      rules: [{ action: 'route', dns_server_address: { local: '192.0.2.1' }, server: 'local' }],
    },
    route: { rules: [], rule_set: [] },
  }
  await installHttpMocks(page, { config })
  await page.goto('/app/rules')
  const dnsSection = page.locator('.rules-section').filter({ has: page.getByRole('heading', { name: 'DNS 分流' }) })
  await dnsSection.locator('.np-resource-card').getByRole('button', { name: '编辑', exact: true }).click()
  const editor = page.getByRole('dialog').last()
  await editor.getByRole('button', { name: 'DNS 高级条件', exact: true }).click()
  await expect(editor.locator('.dns-tag-map code').filter({ hasText: 'local' })).toBeVisible()
  await expect(editor.getByText(/未知 DNS 标签 local/)).toHaveCount(0)
})
