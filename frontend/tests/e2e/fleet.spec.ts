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

test('fleet exposes remote inbound creation without template rollout or template requests', async ({ page }) => {
  await installBaseMocks(page, true)
  const templateRequests: string[] = []
  page.on('request', request => {
    if (new URL(request.url()).pathname.includes('fleetTemplate')) templateRequests.push(request.url())
  })
  await page.route('**/api/fleet**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ servers: [
      { id: 'local', name: '本机', url: '本机', enabled: true, reachable: true, core: { running: true } },
      { id: 'la', name: 'LA', url: 'https://la.example:9999', enabled: true, reachable: true, core: { running: true } },
    ] }),
  }))

  await page.goto('/app/fleet')
  await expect(page.getByRole('heading', { name: '服务器集合' })).toBeVisible()
  await expect(page.getByRole('button', { name: '配置编排' })).toHaveCount(0)
  await page.getByRole('button', { name: '远程新建入站' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: '在远端服务器新建入站' })).toBeVisible()
  await expect(dialog.locator('.fleet-inbound-draft')).toBeVisible()
  await expect(dialog.getByLabel('模板名称')).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: '使用已有模板' })).toHaveCount(0)
  expect(templateRequests).toEqual([])
})

test('remote inbound names account for matching server prefixes outside the current selection', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/fleet**', route => route.fulfill({
    status: 200, contentType: 'application/json',
    body: json({ servers: [
      { id: 'local', name: '本机', url: '本机', enabled: true, reachable: true, core: { running: true } },
      { id: 'la-shared-a', name: 'LA East', url: 'https://la-a.example', enabled: true, reachable: true, core: { running: true } },
      { id: 'la-shared-b', name: 'LA-East', url: 'https://la-b.example', enabled: true, reachable: true, core: { running: true } },
    ] }),
  }))
  await page.route('**/api/fleetInboundTargets**', route => {
    const ids = JSON.parse(new URL(route.request().url()).searchParams.get('targets') ?? '[]') as string[]
    return route.fulfill({
      status: 200, contentType: 'application/json',
      body: json(ids.map(id => ({
        id, name: id, success: true,
        capabilities: { hostname: `${id}.example`, subscriptionHost: `${id}.example`, tlsProfiles: [], inboundTags: [] },
      }))),
    })
  })

  await page.goto('/app/fleet')
  await page.getByRole('button', { name: '远程新建入站' }).click()
  const dialog = page.getByRole('dialog')
  const first = dialog.locator('.fleet-target-grid button').filter({ hasText: 'LA East' })
  const second = dialog.locator('.fleet-target-grid button').filter({ hasText: 'LA-East' })
  await first.click()
  await expect(dialog.getByLabel('入站名称')).toHaveValue('la-east-la-shared-a-socks')
  await second.click()
  await expect(dialog.getByLabel('入站名称').nth(1)).toHaveValue('la-east-la-shared-b-socks')
  await first.click()
  await expect(dialog.getByLabel('入站名称')).toHaveValue('la-east-la-shared-b-socks')
})

test('remote inbound creation starts from a fresh protocol draft and target-local settings', async ({ page }) => {
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
        tlsProfiles: [{ id: id === 'la' ? 101 : 202, name: `tls-${id}`, serverEnabled: true, serverName: `${id}.example`, certificateFingerprint: `${id}1234567890abcdef` }],
        inboundTags: ['existing-hy2'],
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
      tag: target.tag,
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
      tag: target.tag,
      success: true,
      created: true,
    }))) })
  })

  const inboundRequests: string[] = []
  page.on('request', request => {
    const path = new URL(request.url()).pathname
    if (path.endsWith('/api/inbounds')) inboundRequests.push(path)
  })
  await page.goto('/app/fleet')
  await page.getByRole('button', { name: '远程新建入站' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText(/自动关联目标机全部已有本地用户/)).toBeVisible()
  await dialog.locator('.fleet-inbound-draft .v-select .v-field').first().click()
  await page.getByRole('option', { name: 'Hysteria2' }).click()
  await dialog.getByRole('button', { name: /LA/ }).click()
  const names = dialog.getByLabel('入站名称')
  await expect(names).toHaveValue('la-hy2')
  await names.fill('la-stream-hy2')
  await dialog.getByRole('button', { name: /KR/ }).click()
  await expect(dialog.getByText('la.example', { exact: true })).toBeVisible()
  await expect(dialog.getByText('kr.example', { exact: true })).toBeVisible()
  await expect(names.nth(0)).toHaveValue('la-stream-hy2')
  await expect(names.nth(1)).toHaveValue('kr-hy2')

  await names.nth(1).fill('LA-STREAM-HY2')
  await expect(dialog.getByRole('button', { name: '校验全部目标' })).toBeDisabled()
  await expect(dialog.getByText('所选服务器的入站名称不能重复。').first()).toBeVisible()
  await names.nth(1).fill('existing-hy2')
  await expect(dialog.getByText('目标机已存在此名称，请修改。')).toBeVisible()
  await names.nth(1).fill('')
  await expect(dialog.getByText('请输入入站名称。')).toBeVisible()
  await names.nth(1).fill('kr-hy2')

  const ports = dialog.getByLabel('此服务器监听端口')
  await ports.nth(0).fill('8443')
  await ports.nth(1).fill('9443')
  await dialog.getByRole('button', { name: '校验全部目标' }).click()
  await expect(dialog.getByText('全部目标预检通过，尚未创建入站。')).toBeVisible()
  expect(previewPlan).toMatchObject({
    inboundDraft: { type: 'hysteria2' },
    targets: [
      { id: 'la', tag: 'la-stream-hy2', tlsId: 101, listenPort: 8443 },
      { id: 'kr', tag: 'kr-hy2', tlsId: 202, listenPort: 9443 },
    ],
  })
  expect(previewPlan).not.toHaveProperty('sourceInboundId')
  expect(previewPlan).not.toHaveProperty('baseTag')
  expect(previewPlan.inboundDraft).not.toHaveProperty('password')

  await names.nth(0).fill('la-final-hy2')
  await expect(dialog.getByText('全部目标预检通过，尚未创建入站。')).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: '在目标机创建' })).toHaveCount(0)
  await dialog.getByRole('button', { name: '校验全部目标' }).click()
  await expect(dialog.getByText('全部目标预检通过，尚未创建入站。')).toBeVisible()
  expect(previewPlan.targets[0].tag).toBe('la-final-hy2')

  page.once('dialog', dialogEvent => dialogEvent.accept())
  await dialog.getByRole('button', { name: '在目标机创建' }).click()
  await expect(dialog.getByText('已创建，并已自动绑定目标机本地用户。').first()).toBeVisible()
  expect(createPlan).toEqual(previewPlan)
  expect(inboundRequests).toEqual([])
})

test('remote inbound can select the target TLS profile for a fresh VLESS draft', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/fleet**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ servers: [
      { id: 'local', name: '本机', url: '本机', enabled: true, reachable: true, core: { running: true } },
      { id: 'kr', name: 'KR', url: 'https://kr.example:9999', enabled: true, reachable: true, core: { running: true } },
    ] }),
  }))
  await page.route('**/api/fleetInboundTargets**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json([{
      id: 'kr', name: 'KR', success: true,
      capabilities: { hostname: 'kr.panel.example', subscriptionHost: 'kr.example', tlsProfiles: [{ id: 202, name: 'tls-kr', serverEnabled: true, serverName: 'kr.example' }], inboundTags: [] },
    }]),
  }))
  let previewPlan: any
  await page.route('**/api/fleetInboundPreview', async route => {
    const form = new URLSearchParams(route.request().postData() ?? '')
    previewPlan = JSON.parse(form.get('plan') ?? '{}')
    await route.fulfill({ status: 200, contentType: 'application/json', body: json([{
      id: 'kr', name: 'KR', tag: previewPlan.targets[0].tag, success: true, created: false,
      preview: { valid: true, checks: [] },
    }]) })
  })

  await page.goto('/app/fleet')
  await page.getByRole('button', { name: '远程新建入站' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('.fleet-inbound-draft .v-select .v-field').first().click()
  await page.getByRole('option', { name: 'VLESS' }).click()
  await dialog.getByRole('button', { name: /KR/ }).click()
  const name = dialog.getByLabel('入站名称')
  await expect(name).toHaveValue('kr-vless')
  await dialog.locator('.fleet-inbound-draft .v-select .v-field').first().click()
  await page.getByRole('option', { name: 'HTTP', exact: true }).click()
  await expect(name).toHaveValue('kr-http')
  await name.fill('kr-web-custom')
  await dialog.locator('.fleet-inbound-draft .v-select .v-field').first().click()
  await page.getByRole('option', { name: 'VLESS' }).click()
  await expect(name).toHaveValue('kr-web-custom')
  const tls = dialog.getByRole('combobox', { name: '此服务器 TLS 配置' })
  await expect(tls).toBeVisible()
  await dialog.locator('.fleet-remote-target__settings .v-select .v-field').click()
  await page.getByRole('option', { name: /tls-kr/ }).click()
  await dialog.getByRole('button', { name: '校验全部目标' }).click()
  await expect(dialog.getByText('全部目标预检通过，尚未创建入站。')).toBeVisible()
  expect(previewPlan).toMatchObject({ inboundDraft: { type: 'vless' }, targets: [{ id: 'kr', tag: 'kr-web-custom', tlsId: 202 }] })
  expect(previewPlan.targets[0].listenPort).toBeGreaterThanOrEqual(20000)
  expect(previewPlan.targets[0].listenPort).toBeLessThanOrEqual(59999)
  await dialog.locator('.fleet-inbound-draft .v-select .v-field').first().click()
  await page.getByRole('option', { name: 'SOCKS', exact: true }).click()
  await expect(name).toHaveValue('kr-web-custom')
  await expect(dialog.getByText('全部目标预检通过，尚未创建入站。')).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: '在目标机创建' })).toHaveCount(0)
})

test('TLS-required protocols cannot preview without an enabled target TLS profile', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/fleet**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({ servers: [
      { id: 'local', name: '本机', url: '本机', enabled: true, reachable: true, core: { running: true } },
      { id: 'kr', name: 'KR', url: 'https://kr.example:9999', enabled: true, reachable: true, core: { running: true } },
    ] }),
  }))
  await page.route('**/api/fleetInboundTargets**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json([{
      id: 'kr', name: 'KR', success: true,
      capabilities: { hostname: 'kr.panel.example', subscriptionHost: 'kr.example', tlsProfiles: [], inboundTags: [] },
    }]),
  }))

  await page.goto('/app/fleet')
  await page.getByRole('button', { name: '远程新建入站' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('.fleet-inbound-draft .v-select .v-field').first().click()
  await page.getByRole('option', { name: 'Hysteria2' }).click()
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
  await page.route('**/api/fleetInboundTargets**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json([{
      id: 'kr', name: 'KR', success: true,
      capabilities: { hostname: 'kr.panel.example', subscriptionHost: 'kr.example', tlsProfiles: [], inboundTags: [] },
    }]),
  }))
  await page.route('**/api/fleetInboundPreview', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json([{ id: 'kr', name: 'KR', tag: 'kr-masque', success: true, created: false, preview: { valid: true, checks: [] } }]),
  }))

  await page.goto('/app/fleet')
  await page.getByRole('button', { name: '远程新建入站' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('.fleet-inbound-draft .v-select .v-field').first().click()
  await page.getByRole('option', { name: 'MASQUE' }).click()
  await dialog.getByRole('button', { name: /KR/ }).click()
  await expect(dialog.getByText('创建时会在此目标机生成并保存独立的 ECDSA 证书和私钥。')).toBeVisible()
  await expect(dialog.getByRole('combobox', { name: '此服务器 TLS 配置' })).toHaveCount(0)
  await dialog.getByRole('button', { name: '校验全部目标' }).click()
  await expect(dialog.getByText('全部目标预检通过，尚未创建入站。')).toBeVisible()
})
