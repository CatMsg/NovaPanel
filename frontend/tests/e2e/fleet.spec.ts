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

test('remote inbound creation uses per-server names, ports, and TLS profiles', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/fleet**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ servers: [
      { id: 'local', name: '本机', url: '本机', enabled: true, reachable: true, latencyMs: 0, core: { running: true } },
      { id: 'la', name: 'LA', url: 'https://la.example:9999', enabled: true, reachable: true, latencyMs: 20, core: { running: true } },
      { id: 'kr', name: 'KR', url: 'https://kr.example:9999', enabled: true, reachable: true, latencyMs: 30, core: { running: true } },
    ] }),
  }))
  await page.route('**/api/inbounds', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ inbounds: [{ id: 11, tag: 'anytls-main', type: 'anytls', listen_port: 443, tls_id: 1 }] }),
  }))
  await page.route('**/api/fleetInboundTargets**', async route => {
    const url = new URL(route.request().url())
    const targets = JSON.parse(url.searchParams.get('targets') ?? '[]') as string[]
    const items = targets.map(id => ({
      id,
      name: id.toUpperCase(),
      success: true,
      capabilities: {
        hostname: `${id}.panel.example`,
        subscriptionHost: `${id}.example`,
        tlsProfiles: [{ id: id === 'la' ? 101 : 202, name: `tls-${id}`, serverName: `${id}.example`, certificateFingerprint: `${id}1234567890abcdef` }],
      },
    }))
    await route.fulfill({ status: 200, contentType: 'application/json', body: json(items) })
  })

  let previewPlan: any
  let createPlan: any
  await page.route('**/api/fleetInboundPreview', async route => {
    const form = new URLSearchParams(route.request().postData() ?? '')
    previewPlan = JSON.parse(form.get('plan') ?? '{}')
    await route.fulfill({ status: 200, contentType: 'application/json', body: json(previewPlan.targets.map((target: any) => ({
      id: target.id,
      name: target.id.toUpperCase(),
      tag: `${previewPlan.baseTag} [${target.id.toUpperCase()}]`,
      success: true,
      created: false,
      preview: { valid: true, checks: [{ name: '端口', status: 'ok', detail: '可用' }] },
    }))) })
  })
  await page.route('**/api/fleetInboundCreate', async route => {
    const form = new URLSearchParams(route.request().postData() ?? '')
    createPlan = JSON.parse(form.get('plan') ?? '{}')
    await route.fulfill({ status: 200, contentType: 'application/json', body: json(createPlan.targets.map((target: any) => ({
      id: target.id,
      name: target.id.toUpperCase(),
      tag: `${createPlan.baseTag} [${target.id.toUpperCase()}]`,
      success: true,
      created: true,
    }))) })
  })

  await page.goto('/app/fleet')
  await page.getByRole('button', { name: '远程新建入站' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText(/不复制用户绑定/)).toBeVisible()
  await dialog.getByRole('button', { name: /LA/ }).click()
  await dialog.getByRole('button', { name: /KR/ }).click()
  await expect(dialog.getByText('la.example', { exact: true })).toBeVisible()
  await expect(dialog.getByText('kr.example', { exact: true })).toBeVisible()
  await expect(dialog.getByText('将创建为: anytls-main [LA]')).toBeVisible()
  await expect(dialog.getByText('将创建为: anytls-main [KR]')).toBeVisible()

  const ports = dialog.getByLabel('此服务器监听端口')
  await ports.nth(0).fill('8443')
  await ports.nth(1).fill('9443')
  await dialog.getByRole('button', { name: '校验全部目标' }).click()
  await expect(dialog.getByText('全部目标预检通过，尚未创建入站。')).toBeVisible()
  expect(previewPlan).toMatchObject({
    sourceInboundId: 11,
    baseTag: 'anytls-main',
    targets: [
      { id: 'la', tlsId: 101, listenPort: 8443 },
      { id: 'kr', tlsId: 202, listenPort: 9443 },
    ],
  })

  page.once('dialog', dialogEvent => dialogEvent.accept())
  await dialog.getByRole('button', { name: '在目标机创建' }).click()
  await expect(dialog.getByText('已创建。客户端连接前，请在目标机为该入站绑定用户。').first()).toBeVisible()
  expect(createPlan).toEqual(previewPlan)
})

test('remote inbound can add a target TLS profile when the local prototype has no TLS', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/fleet**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ servers: [
      { id: 'local', name: '本机', url: '本机', enabled: true, reachable: true, core: { running: true } },
      { id: 'kr', name: 'KR', url: 'https://kr.example:9999', enabled: true, reachable: true, core: { running: true } },
    ] }),
  }))
  await page.route('**/api/inbounds', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ inbounds: [{ id: 21, tag: 'vless-plain', type: 'vless', listen_port: 443, tls_id: 0 }] }),
  }))
  await page.route('**/api/fleetInboundTargets**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json([{
      id: 'kr', name: 'KR', success: true,
      capabilities: { hostname: 'kr.panel.example', subscriptionHost: 'kr.example', tlsProfiles: [{ id: 202, name: 'tls-kr', serverName: 'kr.example' }] },
    }]),
  }))
  let previewPlan: any
  await page.route('**/api/fleetInboundPreview', async route => {
    const form = new URLSearchParams(route.request().postData() ?? '')
    previewPlan = JSON.parse(form.get('plan') ?? '{}')
    await route.fulfill({ status: 200, contentType: 'application/json', body: json([{
      id: 'kr', name: 'KR', tag: 'vless-plain [KR]', success: true, created: false,
      preview: { valid: true, checks: [] },
    }]) })
  })

  await page.goto('/app/fleet')
  await page.getByRole('button', { name: '远程新建入站' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: /KR/ }).click()
  const tls = dialog.getByRole('combobox', { name: '此服务器 TLS 配置' })
  await expect(tls).toBeVisible()
  await dialog.locator('.fleet-remote-target__settings .v-select').click()
  await page.getByRole('option', { name: /tls-kr/ }).click()
  await dialog.getByRole('button', { name: '校验全部目标' }).click()
  await expect(dialog.getByText('全部目标预检通过，尚未创建入站。')).toBeVisible()
  expect(previewPlan.targets).toEqual([{ id: 'kr', tlsId: 202, listenPort: 443 }])
})

test('TLS-required protocols cannot preview without a target certificate', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/fleet**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ servers: [
      { id: 'local', name: '本机', url: '本机', enabled: true, reachable: true, core: { running: true } },
      { id: 'kr', name: 'KR', url: 'https://kr.example:9999', enabled: true, reachable: true, core: { running: true } },
    ] }),
  }))
  await page.route('**/api/inbounds', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ inbounds: [{ id: 31, tag: 'anytls-plain', type: 'anytls', listen_port: 443, tls_id: 0 }] }),
  }))
  await page.route('**/api/fleetInboundTargets**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json([{
      id: 'kr', name: 'KR', success: true,
      capabilities: { hostname: 'kr.panel.example', subscriptionHost: 'kr.example', tlsProfiles: [] },
    }]),
  }))

  await page.goto('/app/fleet')
  await page.getByRole('button', { name: '远程新建入站' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: /KR/ }).click()
  await expect(dialog.getByText('目标机没有 TLS 配置，请先在该服务器添加。')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '校验全部目标' })).toBeDisabled()
})

test('MASQUE remote creation explains its per-target ECDSA identity', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/fleet**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ servers: [
      { id: 'local', name: '本机', url: '本机', enabled: true, reachable: true, core: { running: true } },
      { id: 'kr', name: 'KR', url: 'https://kr.example:9999', enabled: true, reachable: true, core: { running: true } },
    ] }),
  }))
  await page.route('**/api/inbounds', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ inbounds: [{ id: 41, tag: 'masque-main', type: 'masque', listen_port: 443, tls_id: 0 }] }),
  }))
  await page.route('**/api/fleetInboundTargets**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json([{
      id: 'kr', name: 'KR', success: true,
      capabilities: { hostname: 'kr.panel.example', subscriptionHost: 'kr.example', tlsProfiles: [] },
    }]),
  }))
  await page.route('**/api/fleetInboundPreview', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json([{ id: 'kr', name: 'KR', tag: 'masque-main [KR]', success: true, created: false, preview: { valid: true, checks: [] } }]),
  }))

  await page.goto('/app/fleet')
  await page.getByRole('button', { name: '远程新建入站' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: /KR/ }).click()
  await expect(dialog.getByText('创建时会在此目标机生成并保存独立的 ECDSA 证书和私钥。')).toBeVisible()
  await expect(dialog.getByRole('combobox', { name: '此服务器 TLS 配置' })).toHaveCount(0)
  await dialog.getByRole('button', { name: '校验全部目标' }).click()
  await expect(dialog.getByText('全部目标预检通过，尚未创建入站。')).toBeVisible()
})
