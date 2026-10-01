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

test('client traffic cycle start is editable and recalculates the next reset', async ({ page }) => {
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

  const cycleStart = page.getByTestId('client-reset-cycle-start').locator('input')
  await expect(cycleStart).toBeVisible()
  await expect(cycleStart).not.toHaveValue('')
  const newStart = await page.evaluate(() => {
    const date = new Date(Date.now() - 86400_000)
    const pad = (value: number) => String(value).padStart(2, '0')
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
  })
  await cycleStart.fill(newStart)
  await page.getByRole('button', { name: '重置', exact: true }).click()
  const expectedNextReset = await cycleStart.evaluate((element) =>
    Math.floor(new Date((element as HTMLInputElement).value).getTime() / 1000) + 30 * 86400,
  )
  await page.getByRole('button', { name: '保存', exact: true }).last().click()
  await expect.poll(() => savedClient?.nextReset).toBe(expectedNextReset)
  expect(savedClient?.resetScheduleChanged).toBe(true)
  expect(savedClient?.resetUsage).toBe(true)
})
