import { expect, test } from '@playwright/test'
import { installBaseMocks, json } from './mockApi'

test('fleet loads status and renders billing-cycle traffic history', async ({ page }) => {
  await installBaseMocks(page, true)
  const trafficBudget = {
    enabled: true, supported: true, limitBytes: 500000000000, reserveBytes: 50000000000,
    clientPoolBytes: 450000000000, usedBytes: 120000000000, meteredRxBytes: 90000000000,
    meteredTxBytes: 120000000000, offsetBytes: 0, accountingMode: 'tx', level: 'normal', blocked: false,
  }
  await page.route('**/api/fleet**', route => route.fulfill({
    status: 200, contentType: 'application/json',
    body: json({
      checkedAt: '2026-09-23T06:00:00Z',
      servers: [{
        id: 'local', name: '本机', url: '本机', enabled: true, reachable: true, latencyMs: 0,
        resourcesReady: true, networkTotalsReady: true, cpuPercent: 12, memoryUsed: 1024, memoryTotal: 4096,
        networkSent: 1000, networkReceived: 2000, onlineUsers: 1, clients: 2, inbounds: 1, outbounds: 1, endpoints: 0,
        listeners: 2, natRules: 0, system: { appVersion: '1.6.118' }, core: { running: true }, trafficBudget,
      }],
    }),
  }))
  await page.route('**/api/fleetTrafficHistory**', route => route.fulfill({
    status: 200, contentType: 'application/json',
    body: json({
      periodStart: '2026-09-01T00:00:00Z', periodEnd: '2026-10-01T00:00:00Z',
      samples: [
        { dateTime: 1788220800, periodStart: 1788220800, meteredRxBytes: 1000000000, meteredTxBytes: 2000000000, usedBytes: 2000000000, clientPoolBytes: 450000000000, level: 'normal' },
        { dateTime: 1788221100, periodStart: 1788220800, meteredRxBytes: 1500000000, meteredTxBytes: 2500000000, usedBytes: 2500000000, clientPoolBytes: 450000000000, level: 'normal' },
      ],
    }),
  }))
  await page.route('**/api/fleetAction**', route => route.fulfill({ status: 200, contentType: 'application/json', body: json({ state: 'idle' }) }))

  await page.goto('/app/fleet')
  await expect(page.getByRole('heading', { name: '服务器集合' })).toBeVisible()
  await expect(page.getByText('本周期总流量').first()).toBeVisible()
  await page.getByRole('button', { name: '详情' }).first().click()
  await expect(page.getByText('本周期流量趋势')).toBeVisible()
  await expect(page.getByText('2 个采样点')).toBeVisible()
})

test('configuration rollout separates using a saved template from creating one', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/fleet**', route => route.fulfill({
    status: 200, contentType: 'application/json',
    body: json({ servers: [{ id: 'local', name: '本机', url: '本机', enabled: true, reachable: true, core: { running: true } }] }),
  }))
  await page.route('**/api/fleetTemplates**', route => route.fulfill({ status: 200, contentType: 'application/json', body: json([]) }))
  await page.route('**/api/inbounds', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ inbounds: [
      { id: 11, tag: 'vless-main', type: 'vless', listen_port: 443 },
      { id: 12, tag: 'hy2-main', type: 'hysteria2', listen_port: 8443 },
    ] }),
  }))
  let capturedInboundIds: number[] | undefined
  let capturedSections: Record<string, boolean> | undefined
  await page.route('**/api/fleetTemplateCapture', async route => {
    const form = new URLSearchParams(route.request().postData() ?? '')
    capturedInboundIds = JSON.parse(form.get('inboundIds') ?? '[]')
    capturedSections = JSON.parse(form.get('sections') ?? '{}')
    await route.fulfill({ status: 200, contentType: 'application/json', body: json({ id: 'template-1', name: 'one inbound', sections: capturedSections }) })
  })
  await page.route('**/api/fleetTemplatePreview', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json([{
      id: 'local', name: '本机', success: true,
      preview: { inboundAdd: 0, inboundUpdate: 1, inboundUpdateTags: ['hy2-main'] },
    }]),
  }))

  await page.goto('/app/fleet')
  await page.getByRole('button', { name: '配置编排' }).click()

  const dialog = page.getByRole('dialog')
  await expect(dialog.getByLabel('模板名称')).toBeVisible()
  await dialog.getByRole('button', { name: '使用已有模板' }).click()
  await expect(dialog.getByText('还没有保存的模板，请在下方创建第一个模板。')).toBeVisible()
  await expect(dialog.getByLabel('模板名称')).toBeHidden()
  await dialog.getByRole('button', { name: '新建模板' }).click()
  await expect(dialog.getByLabel('模板名称')).toBeVisible()
  await expect(dialog.getByText('已选 2/2 个入站')).toBeVisible()
  await dialog.getByRole('checkbox', { name: /vless-main/ }).uncheck()
  await expect(dialog.getByText('已选 1/2 个入站')).toBeVisible()
  await dialog.getByLabel('模板名称').fill('one inbound')
  await dialog.getByRole('button', { name: '保存并继续' }).click()
  await expect(dialog.getByRole('heading', { name: '选择目标服务器' })).toBeVisible()
  expect(capturedInboundIds).toEqual([12])
  expect(capturedSections).toMatchObject({ inbounds: true, clients: false, route: false, dns: false, outbounds: false })
  await dialog.getByRole('button', { name: /本机/ }).click()
  await dialog.getByRole('button', { name: '预览差异' }).click()
  await expect(dialog.getByText('更新： hy2-main')).toBeVisible()
})
