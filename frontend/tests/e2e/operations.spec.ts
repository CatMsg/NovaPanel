import { expect, test, type Locator, type Page, type Route } from '@playwright/test'
import { installBaseMocks, json } from './mockApi'

const inbound: Record<string, unknown> = {
  id: 31,
  type: 'socks',
  tag: 'subscription-target',
  listen: '127.0.0.1',
  listen_port: 1080,
  tls_id: 0,
  users: [],
  addrs: [],
  out_json: {},
  exclude_from_sub: false,
}

function loadData(inbounds: Record<string, unknown>[] = [inbound]) {
  return {
    config: {},
    lastUpdate: 1,
    onlines: { inbound: [], outbound: [], user: [] },
    clients: [],
    inbounds,
    outbounds: [],
    endpoints: [],
    services: [],
    tls: [],
    enableTraffic: false,
  }
}

const runtimeStatus = {
  sbd: { running: true, stats: { Alloc: 1, NumGoroutine: 1, Uptime: 1 } },
  sys: { appVersion: '1.6.130', bootTime: Math.floor(Date.now() / 1000) - 60 },
}

function backupStatus(settings = {
  enabled: false,
  intervalMinutes: 1440,
  retention: 7,
  destination: '',
  username: '',
  hasPassword: false,
}) {
  return {
    settings: { ...settings },
    state: {
      lastSuccessAt: '2026-10-12T06:00:00Z',
      lastOperation: 'backup',
      lastFinishedAt: '2026-10-12T06:00:00Z',
      nextDueAt: '',
      lastError: '',
      retentionWarning: '',
    },
    busy: false,
    schedulerRunning: false,
    namespace: 'fixture-instance',
  }
}

async function fulfillJson(route: Route, obj: unknown, msg = '', success = true) {
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json(obj, msg, success),
  })
}

async function installAppBase(page: Page, getLoadData: () => unknown) {
  await page.addInitScript(() => localStorage.setItem('reloadItems:v2', 'i-sbd'))
  await installBaseMocks(page)
  await page.route('**/api/load**', route => fulfillJson(route, getLoadData()))
}

async function expectNoHorizontalOverflow(page: Page) {
  const sizes = await page.evaluate(() => ({
    viewport: document.documentElement.clientWidth,
    document: document.documentElement.scrollWidth,
    body: document.body.scrollWidth,
  }))
  expect(Math.max(sizes.document, sizes.body)).toBeLessThanOrEqual(sizes.viewport)
}

async function expectDialogInsideViewport(page: Page, dialog: Locator) {
  const bounds = await dialog.boundingBox()
  expect(bounds).not.toBeNull()
  if (!bounds) return
  const viewport = await page.evaluate(() => ({ width: innerWidth, height: innerHeight }))
  expect(bounds.x).toBeGreaterThanOrEqual(0)
  expect(bounds.y).toBeGreaterThanOrEqual(0)
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(viewport.width + 1)
  expect(bounds.y + bounds.height).toBeLessThanOrEqual(viewport.height + 1)
}

test('home uses clear placeholders when system metadata is unavailable', async ({ page }) => {
  await installAppBase(page, () => loadData())
  await page.route('**/api/status**', route => fulfillJson(route, { sys: {}, sbd: { running: false } }))
  await page.route('**/api/maintenance-status', route => fulfillJson(route, { enabled: false, transitioning: false }))

  await page.goto('/app/')

  const systemTile = page.locator('.main-tile').filter({ hasText: '系统信息' })
  await expect(systemTile.locator('.main-info-grid__value')).toHaveText(['-', '-', '-', '-', '-', '-'])
  await expect(page.locator('.main-hero__status-row').filter({ hasText: '运行时间' }).locator('.main-hero__status-row-value')).toHaveText('-')
  await expect(page.locator('.main-hero__status-row').filter({ hasText: '防火墙' }).locator('.main-hero__status-row-value')).toHaveText('无')
  await expect(page.locator('.main-hero__status-row').filter({ hasText: '运行状态' }).locator('.main-hero__status-row-value')).toHaveText('已停止')
  const runtimeTile = page.locator('.main-tile').filter({ hasText: '运行信息' })
  await expect(runtimeTile.locator('.main-info-grid__value').first()).toHaveText('已停止')
})

test('subscription hiding saves its flag and updates the inbound list without restarting the service', async ({ page }) => {
  let inbounds = [inbound]
  let preflightInbound: Record<string, unknown> | undefined
  let savedInbound: Record<string, unknown> | undefined
  let restartCalls = 0
  await installAppBase(page, () => loadData(inbounds))
  await page.route('**/api/inbounds**', route => fulfillJson(route, { inbounds }))
  await page.route('**/api/preflight**', async route => {
    const body = route.request().postDataJSON() as { data: string }
    preflightInbound = JSON.parse(body.data) as Record<string, unknown>
    await fulfillJson(route, { changed: true, warnings: [] })
  })
  await page.route('**/api/save**', async route => {
    const body = route.request().postDataJSON() as { data: string }
    savedInbound = JSON.parse(body.data) as Record<string, unknown>
    inbounds = [savedInbound]
    await fulfillJson(route, loadData(inbounds))
  })
  await page.route('**/api/status**', route => fulfillJson(route, runtimeStatus))
  await page.route('**/api/maintenance-status', route => fulfillJson(route, { enabled: false, transitioning: false }))
  await page.route('**/api/restartSb**', async route => {
    restartCalls++
    await fulfillJson(route, {})
  })

  await page.goto('/app/inbounds')
  const row = page.getByRole('row', { name: /subscription-target/ })
  await expect(row).toBeVisible()
  await row.getByRole('button', { name: '编辑' }).click()

  const editor = page.getByRole('dialog').last()
  const visibility = editor.getByRole('checkbox', { name: /从订阅隐藏/ })
  await expect(visibility).toBeVisible()
  await expect(visibility).not.toBeChecked()
  await visibility.check()
  await editor.getByRole('button', { name: '保存', exact: true }).click()

  await expect.poll(() => savedInbound?.exclude_from_sub).toBe(true)
  expect(preflightInbound?.exclude_from_sub).toBe(true)
  await expect(row.getByText(/订阅已隐藏/)).toBeVisible()
  expect(restartCalls).toBe(0)

  await page.goto('/app/')
  const runtimeRow = page.locator('.main-hero__status-row').filter({ hasText: '运行状态' })
  await expect(runtimeRow.locator('.main-hero__status-row-value')).toHaveText('运行中')
  expect(restartCalls).toBe(0)
})

for (const width of [1280, 390]) {
  test(`new MASQUE inbound defaults to dual subscriptions at ${width}px`, async ({ page }) => {
    await installAppBase(page, () => loadData([]))
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/app/inbounds')
    await page.getByRole('button', { name: '添加', exact: true }).click()

    const editor = page.getByRole('dialog').last()
    await editor.getByRole('combobox').first().click()
    await page.getByRole('option', { name: 'Masque', exact: true }).click()

    const compatibility = editor.getByRole('checkbox', { name: '兼容 Clash / Mihomo 订阅（密钥认证）' })
    await expect(compatibility).toBeChecked()
    await expect(editor.getByText(/默认同时支持 sing-box JSON 与 Clash \/ Mihomo 订阅/)).toBeVisible()
    await expect(editor.getByText(/无需选择 TLS 模板/)).toBeVisible()
    await expect(editor.getByText(/自动生成并保存此入站专用的 ECDSA 证书与密钥/)).toBeVisible()
    await expectDialogInsideViewport(page, editor)
    await expectNoHorizontalOverflow(page)
  })

  test(`native-only MASQUE inbound can enable Mihomo subscriptions at ${width}px`, async ({ page }) => {
    const masque = { ...inbound, type: 'masque', tag: 'dual-masque', tls_id: 7, listen_port: 443, version: [3], system: false, mtu: 1280, mihomo_compatible: false }
    let saved: Record<string, unknown> | undefined
    await installAppBase(page, () => ({ ...loadData([masque]), tls: [{ id: 7, name: 'ECDSA TLS', server: { enabled: true } }] }))
    await page.route('**/api/inbounds**', route => fulfillJson(route, { inbounds: [masque] }))
    await page.route('**/api/preflight**', route => fulfillJson(route, { changed: true, warnings: [] }))
    await page.route('**/api/save**', async route => {
      saved = JSON.parse(route.request().postDataJSON().data)
      await fulfillJson(route, {})
    })
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/app/inbounds')
    if (width === 1280) await page.getByRole('row', { name: /dual-masque/ }).getByRole('button', { name: '编辑' }).click()
    else await page.locator('.resource-card').filter({ hasText: 'dual-masque' }).getByRole('button', { name: '编辑' }).click()
    const editor = page.getByRole('dialog').last()
    await expect(editor.getByText('ECDSA TLS', { exact: true })).toHaveCount(0)
    const toggle = editor.getByRole('checkbox', { name: '兼容 Clash / Mihomo 订阅（密钥认证）' })
    await expect(toggle).not.toBeChecked()
    await toggle.check()
    await expect(editor.getByText(/ECDSA 证书由入站独立管理/)).toBeVisible()
    await expect(editor.getByText(/多台设备同时连接并共用该用户的隧道地址/)).toBeVisible()
    await expectDialogInsideViewport(page, editor)
    await expectNoHorizontalOverflow(page)
    await editor.getByRole('button', { name: '保存', exact: true }).click()
    await expect.poll(() => saved?.mihomo_compatible).toBe(true)
    expect(saved?.type).toBe('masque')
    expect(saved).not.toHaveProperty('private_key')
  })
}

test('maintenance pause requires confirmation, persists status, and reports a failed resume', async ({ page }) => {
  let maintenance = { enabled: false, transitioning: false }
  const actions: boolean[] = []
  let statusReads = 0
  await installAppBase(page, () => loadData())
  await page.route('**/api/maintenance-status', async route => {
    statusReads++
    await fulfillJson(route, maintenance)
  })
  await page.route('**/api/maintenance', async route => {
    const body = route.request().postDataJSON() as { enabled: boolean }
    actions.push(body.enabled)
    if (body.enabled) maintenance = { enabled: true, transitioning: false }
    await fulfillJson(route, null, body.enabled ? '' : 'resume rejected for test', body.enabled)
  })

  await page.goto('/app/')
  const action = page.getByTestId('maintenance-action')
  await expect(action).toBeEnabled()
  await action.click()
  const confirmation = page.getByRole('dialog').last()
  await expect(page.getByTestId('maintenance-pause-confirmation')).toBeVisible()
  expect(actions).toHaveLength(0)
  await page.getByTestId('maintenance-pause-confirm').click()

  await expect.poll(() => actions).toEqual([true])
  const status = page.getByTestId('maintenance-status')
  await expect(status).toHaveClass(/maintenance-status--paused/)
  expect(statusReads).toBeGreaterThanOrEqual(2)

  await page.reload()
  await expect(status).toHaveClass(/maintenance-status--paused/)
  await page.getByTestId('maintenance-action').click()
  await expect.poll(() => actions).toEqual([true, false])
  await expect(page.getByTestId('maintenance-error')).toContainText('resume rejected for test')
  await expect(status).toHaveClass(/maintenance-status--paused/)
})

test('remote backup is opt-in, keeps its secret write-only, disables unconfigured actions, and rehearses without importing', async ({ page }) => {
  const secret = 'fixture-only-remote-secret'
  let settings = {
    enabled: false,
    intervalMinutes: 1440,
    retention: 7,
    destination: '',
    username: '',
    hasPassword: false,
  }
  const statusPayloads: unknown[] = []
  const runActions: Array<{ action: string; artifact: string }> = []
  let savedPayload: Record<string, unknown> | undefined
  let importDbCalls = 0
  await installAppBase(page, () => loadData())
  await page.route('**/api/remote-backup-status**', async route => {
    const payload = backupStatus(settings)
    statusPayloads.push(payload)
    await fulfillJson(route, payload)
  })
  await page.route('**/api/remoteBackupSave**', async route => {
    savedPayload = route.request().postDataJSON() as Record<string, unknown>
    settings = {
      enabled: savedPayload.enabled === true,
      intervalMinutes: Number(savedPayload.intervalMinutes),
      retention: Number(savedPayload.retention),
      destination: String(savedPayload.destination),
      username: String(savedPayload.username),
      hasPassword: typeof savedPayload.password === 'string' && savedPayload.password.length > 0,
    }
    await fulfillJson(route, backupStatus(settings))
  })
  await page.route('**/api/remoteBackupRun**', async route => {
    const body = route.request().postDataJSON() as { action: string; artifact: string }
    runActions.push(body)
    if (body.action === 'list') {
      await fulfillJson(route, {
        message: '备份列表已读取',
        artifacts: [{ artifact: 'fixture-backup.db', size: 2048, sha256: 'a'.repeat(64) }],
      })
    } else if (body.action === 'rehearse') {
      await fulfillJson(route, { message: '演练成功，当前数据库未修改', summary: { settings: 1, clients: 2 } })
    } else {
      await fulfillJson(route, { message: `unexpected action: ${body.action}` }, '', false)
    }
  })
  await page.route('**/api/importdb**', async route => {
    importDbCalls++
    await fulfillJson(route, {}, 'unexpected import', false)
  })

  await page.goto('/app/')
  await page.getByRole('button', { name: '备份与恢复', exact: true }).click()
  const dialog = page.getByRole('dialog').last()
  const panel = dialog.locator('.offhost-backup')
  await expect(panel).toBeVisible()
  await expect(panel.locator('.offhost-backup__status')).toContainText('调度器未运行')
  const enabled = panel.getByRole('checkbox', { name: '启用自动备份（保存后生效）' })
  await expect(enabled).not.toBeChecked()
  await expect(panel.locator('.offhost-backup__status')).toContainText('已关闭')
  await expect(panel.getByRole('button', { name: '读取备份列表', exact: true })).toBeDisabled()
  await expect(panel.getByRole('button', { name: '立即异地备份', exact: true })).toBeDisabled()

  await enabled.check()
  await panel.getByRole('textbox', { name: 'WebDAV 目标目录 HTTPS URL' }).fill('https://storage.example/dav/backups/')
  await panel.getByRole('textbox', { name: 'WebDAV 用户名' }).fill('fixture-operator')
  const password = panel.locator('input[type="password"]')
  await password.fill(secret)
  await panel.getByRole('button', { name: '保存设置', exact: true }).click()

  await expect.poll(() => savedPayload?.password).toBe(secret)
  expect(savedPayload).toMatchObject({
    enabled: true,
    destination: 'https://storage.example/dav/backups/',
    username: 'fixture-operator',
    clearPassword: false,
  })
  await expect(password).toHaveValue('')
  await expect(panel).not.toContainText(secret)
  expect(statusPayloads.every(payload => !JSON.stringify(payload).includes(secret))).toBe(true)
  await expect(panel.locator('.offhost-backup__status')).toContainText('fixture-instance')

  await panel.getByRole('button', { name: '读取备份列表', exact: true }).click()
  await expect.poll(() => runActions.map(item => item.action)).toEqual(['list'])
  const artifact = panel.getByRole('combobox', { name: '选择本实例的已提交备份' })
  await expect(artifact).toHaveValue('fixture-backup.db')
  await panel.getByRole('button', { name: '非破坏性恢复演练', exact: true }).click()
  await expect.poll(() => runActions.map(item => item.action)).toEqual(['list', 'rehearse'])
  expect(runActions[1]).toEqual({ action: 'rehearse', artifact: 'fixture-backup.db' })
  await expect(panel).toContainText('演练成功，当前数据库未修改')
  expect(importDbCalls).toBe(0)
})

test('390px viewport keeps inbound, maintenance, and backup dialogs inside without horizontal overflow', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await installAppBase(page, () => loadData())
  await page.route('**/api/inbounds**', route => fulfillJson(route, { inbounds: [inbound] }))
  await page.route('**/api/maintenance-status**', route => fulfillJson(route, { enabled: false, transitioning: false }))
  await page.route('**/api/remote-backup-status**', route => fulfillJson(route, backupStatus()))

  await page.goto('/app/inbounds')
  const card = page.locator('.resource-card').filter({ hasText: String(inbound.tag) })
  await card.getByRole('button', { name: '编辑' }).click()
  const inboundDialog = page.getByRole('dialog').last()
  await expect(inboundDialog).toBeVisible()
  await expectDialogInsideViewport(page, inboundDialog)
  await expectNoHorizontalOverflow(page)

  await page.goto('/app/')
  await page.getByTestId('maintenance-action').click()
  const maintenanceDialog = page.getByRole('dialog').last()
  await expect(maintenanceDialog).toBeVisible()
  await expectDialogInsideViewport(page, maintenanceDialog)
  await expectNoHorizontalOverflow(page)
  await maintenanceDialog.getByRole('button').first().click()

  await page.getByRole('button', { name: '备份与恢复', exact: true }).click()
  const backupDialog = page.getByRole('dialog').last()
  await expect(backupDialog.locator('.offhost-backup')).toBeVisible()
  await expectDialogInsideViewport(page, backupDialog)
  await expectNoHorizontalOverflow(page)
})
