import { expect, test, type Page } from '@playwright/test'
import { installBaseMocks, json } from './mockApi'

const pageData = {
  config: {},
  lastUpdate: 1,
  onlines: { inbound: [], outbound: [], user: [] },
  clients: [{
    id: 17,
    enable: true,
    name: 'alice',
    inbounds: [],
    volume: 0,
    expiry: 0,
    up: 0,
    down: 0,
    desc: '',
    group: '',
    uploadLimit: 0,
    downloadLimit: 0,
  }],
  inbounds: [],
  outbounds: [],
  endpoints: [],
  services: [],
  tls: [],
  enableTraffic: false,
}

async function expectClientActionIcons(page: Page) {
  const fontStatuses = await page.evaluate(async () =>
    (await document.fonts.load('24px "Material Design Icons"')).map(font => font.status),
  )
  expect(fontStatuses).toContain('loaded')

  for (const iconName of ['mdi-link-variant', 'mdi-share-variant-outline']) {
    const icon = page.locator(`.mdi.${iconName}`).last()
    await expect(icon).toBeVisible()

    const glyph = await icon.evaluate(element => ({
      content: getComputedStyle(element, '::before').content,
      fontFamily: getComputedStyle(element, '::before').fontFamily,
    }))

    expect(glyph.content).not.toBe('none')
    expect(glyph.content).not.toBe('normal')
    expect(glyph.fontFamily).toContain('Material Design Icons')
  }
}

test('browsing history remains available when traffic charts are disabled', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/load**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json(pageData),
  }))

  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('/app/clients')
  await page.getByRole('button', { name: '更多操作' }).first().click()
  await expectClientActionIcons(page)
  await expect(page.getByText('浏览记录', { exact: true })).toBeVisible()
  await expect(page.getByText('流量图表', { exact: true })).toHaveCount(0)

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.locator('.clients-mobile-card').filter({ hasText: 'alice' })).toBeVisible()
  await page.getByRole('button', { name: '更多操作' }).first().click()
  await expectClientActionIcons(page)
  await expect(page.getByText('浏览记录', { exact: true })).toBeVisible()
  await expect(page.getByText('流量图表', { exact: true })).toHaveCount(0)
})

test('client auto-reset is fixed to the first of each month', async ({ page }) => {
  await installBaseMocks(page, true)
  const client = {
    ...pageData.clients[0],
    config: {},
    links: [],
    history: [],
    autoReset: true,
    resetDays: 30,
    nextReset: Math.floor(Date.now() / 1000) + 13 * 86400,
    totalUp: 0,
    totalDown: 0,
  }
  let savedClient: Record<string, unknown> | undefined
  await page.route('**/api/load**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ ...pageData, clients: [client] }),
  }))
  await page.route('**/api/clients**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ clients: [client] }),
  }))
  await page.route('**/api/preflight**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ changed: true, warnings: [] }),
  }))
  await page.route('**/api/save**', async route => {
    savedClient = JSON.parse(route.request().postDataJSON().data)
    await route.fulfill({ status: 200, contentType: 'application/json', body: json({}) })
  })

  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('/app/clients')
  const clientRow = page.getByRole('row', { name: /alice/ })
  await expect(clientRow).toBeVisible()
  const clientLoad = page.waitForRequest(request => {
    const url = new URL(request.url())
    return url.pathname.endsWith('/api/clients') && url.searchParams.get('id') === '17'
  })
  await clientRow.getByRole('button', { name: '编辑', exact: true }).click()
  await clientLoad
  await expect(page.getByText('编辑 客户端', { exact: true })).toBeVisible()

  await expect(page.getByTestId('client-reset-monthly')).toContainText('每月 1 日 00:00 按面板时区重置')
  await expect(page.locator('input[type="datetime-local"]')).toHaveCount(0)
  await expect(page.getByLabel('首次使用后有效天数')).toHaveCount(0)
  await page.getByRole('button', { name: '保存', exact: true }).last().click()
  await expect.poll(() => savedClient?.nextReset).toBe(client.nextReset)
  expect(savedClient?.resetDays).toBe(30)
})

const shareClient = {
  ...pageData.clients[0],
  config: {},
  links: [
    { type: 'local', remark: 'JP Mieru', uri: 'mieru://test-jp' },
    { type: 'external', remark: 'LA VLESS', uri: 'vless://test-la' },
  ],
}
const subURI = 'https://example.test/sub/'

async function installShareMocks(page: Page, clients = [shareClient], subscription = subURI) {
  await installBaseMocks(page, true)
  await page.route('**/api/load**', route => route.fulfill({
    status: 200, contentType: 'application/json', body: json({ ...pageData, clients, subURI: subscription }),
  }))
  await page.route('**/api/clients**', route => {
    const id = Number(new URL(route.request().url()).searchParams.get('id'))
    return route.fulfill({ status: 200, contentType: 'application/json', body: json({ clients: clients.filter(client => client.id === id) }) })
  })
}

async function openShare(page: Page, name = 'alice') {
  await expect(page.getByText(name, { exact: true }).first()).toBeVisible()
  const row = page.getByRole('row', { name: new RegExp(name) })
  if (await row.isVisible()) await row.getByRole('button', { name: '更多操作' }).click()
  else await page.locator('.clients-mobile-card').filter({ hasText: name }).getByRole('button', { name: '更多操作' }).click()
  await page.getByText('分享用户', { exact: true }).click()
  return page.locator('#qrcode-modal')
}

for (const width of [1280, 390]) {
  test(`sharing cold-open shows a single QR and switches formats at ${width}px`, async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await installShareMocks(page)
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/app/clients')
    const modal = await openShare(page)
    const url = modal.getByTestId('share-url').locator('textarea').first()
    await expect(url).toHaveValue(subURI + 'alice?format=clash')
    await expect(modal.getByTestId('share-qr')).toHaveCount(1)
    await expect(modal.getByTestId('share-qr').locator('svg')).toBeVisible()
    await modal.getByRole('button', { name: 'JSON', exact: true }).click()
    await expect(url).toHaveValue(subURI + 'alice?format=json')
    await modal.getByRole('button', { name: 'sing-box', exact: true }).click()
    await expect(url).toHaveValue('sing-box://import-remote-profile?url=' + encodeURIComponent(subURI + 'alice?format=json') + '#alice')
    await expect(modal.getByTestId('share-format-hint')).toContainText('JSON 订阅')
    await modal.getByRole('button', { name: '自动识别', exact: true }).click()
    await expect(url).toHaveValue(subURI + 'alice')
    await modal.getByRole('button', { name: '复制当前链接' }).click()
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(subURI + 'alice')
    await modal.getByRole('tab', { name: '链接', exact: true }).click()
    await expect(url).toHaveValue('mieru://test-jp')
    await modal.getByRole('combobox', { name: '选择节点' }).press('ArrowDown')
    await page.getByRole('option', { name: 'LA VLESS' }).click()
    await expect(url).toHaveValue('vless://test-la')
    await expect(modal.getByTestId('share-qr')).toHaveCount(1)
    const geometry = await modal.evaluate(element => ({
      width: element.getBoundingClientRect().width,
      scrollWidth: element.scrollWidth,
      bottom: element.getBoundingClientRect().bottom,
      viewport: innerHeight,
    }))
    expect(geometry.scrollWidth).toBeLessThanOrEqual(Math.ceil(geometry.width))
    expect(geometry.bottom).toBeLessThanOrEqual(geometry.viewport)
    await expect(modal.getByRole('button', { name: '复制当前链接' })).toBeInViewport()
  })
}

test('sharing never replaces the current user with a late response from a closed dialog', async ({ page }) => {
  const bob = { ...shareClient, id: 18, name: 'bob' }
  await installShareMocks(page, [shareClient, bob])
  let releaseFirst!: () => void
  const gate = new Promise<void>(resolve => { releaseFirst = resolve })
  let firstRequested = false
  await page.route('**/api/clients?id=17', async route => {
    firstRequested = true
    await gate
    await route.fulfill({ status: 200, contentType: 'application/json', body: json({ clients: [shareClient] }) })
  })
  await page.goto('/app/clients')
  const modal = await openShare(page)
  await expect.poll(() => firstRequested).toBe(true)
  await modal.getByRole('button', { name: '关闭', exact: true }).first().click()
  await openShare(page, 'bob')
  const url = modal.getByTestId('share-url').locator('textarea').first()
  await expect(url).toHaveValue(subURI + 'bob?format=clash')
  const firstResponse = page.waitForResponse(response => response.url().endsWith('/api/clients?id=17'))
  releaseFirst()
  await firstResponse
  await expect(url).toHaveValue(subURI + 'bob?format=clash')
})

test('sharing fails closed, allows a retry, and keeps long links copyable without rendering a QR', async ({ page }) => {
  await installShareMocks(page, [shareClient], subURI + 'x'.repeat(2100) + '/')
  let attempts = 0
  await page.route('**/api/clients**', route => {
    attempts++
    return route.fulfill({ status: 200, contentType: 'application/json', body: json({ clients: attempts === 1 ? [] : [shareClient] }) })
  })
  await page.goto('/app/clients')
  const modal = await openShare(page)
  await expect(modal.getByText('用户加载失败，请重新加载后再分享。')).toBeVisible()
  await expect(modal.getByRole('button', { name: '复制当前链接' })).toBeDisabled()
  await modal.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(modal.getByText('链接过长，无法可靠生成二维码，请复制链接导入。')).toBeVisible()
  await expect(modal.getByTestId('share-qr')).toHaveCount(0)
  await expect(modal.getByRole('button', { name: '复制当前链接' })).toBeEnabled()
})

test('sharing explains a missing subscription address and empty node list without an empty QR', async ({ page }) => {
  await installShareMocks(page, [{ ...shareClient, links: [] }], '')
  await page.goto('/app/clients')
  const modal = await openShare(page)
  await expect(modal.getByText('未配置订阅地址，请先在设置中配置。')).toBeVisible()
  await expect(modal.getByTestId('share-qr')).toHaveCount(0)
  await expect(modal.getByRole('button', { name: '复制当前链接' })).toBeDisabled()
  await modal.getByRole('tab', { name: '链接', exact: true }).click()
  await expect(modal.getByText('该用户没有可分享的节点链接。')).toBeVisible()
})

test('pause and credential rotation preserve the user and its protocol configuration', async ({ page }) => {
  await installShareMocks(page)
  let current = {
    ...shareClient,
    volume: 123456,
    config: { mieru: { password: 'old-password', username: 'alice' }, vless: { uuid: 'old-uuid' } },
  }
  const writes: typeof current[] = []
  await page.route('**/api/clients**', route => route.fulfill({
    status: 200, contentType: 'application/json', body: json({ clients: [current] }),
  }))
  await page.route('**/api/preflight**', route => route.fulfill({
    status: 200, contentType: 'application/json', body: json({ changed: true, warnings: [] }),
  }))
  await page.route('**/api/save**', async route => {
    current = JSON.parse(route.request().postDataJSON().data)
    writes.push(current)
    await route.fulfill({ status: 200, contentType: 'application/json', body: json({ clients: [current] }) })
  })
  await page.goto('/app/clients')
  const modal = await openShare(page)
  await modal.getByRole('button', { name: '暂停访问' }).click()
  await expect(modal.getByRole('button', { name: '恢复访问' })).toBeEnabled()
  expect(writes).toHaveLength(1)
  expect(writes[0].config.mieru.password).toBe('old-password')
  page.once('dialog', dialog => dialog.accept())
  await modal.getByRole('button', { name: '撤销旧凭据' }).click()
  await expect.poll(() => writes.length).toBe(2)
  expect(writes[1].config.mieru.password).not.toBe('old-password')
  expect(writes[1].config.vless.uuid).not.toBe('old-uuid')
  expect(writes[1].config.mieru.username).toBe('alice')
  expect(writes[1]).toMatchObject({ id: 17, name: 'alice', enable: false, volume: 123456 })
})
