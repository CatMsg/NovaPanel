import { expect, test } from '@playwright/test'
import { installBaseMocks, json } from './mockApi'

test('live sessions show and search source IP', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/sessions**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({
      checkedAt: '2026-09-23T11:00:00Z',
      sessions: [{
        id: 'core:test-session',
        serverId: 'local',
        serverName: '本机',
        kind: 'sing-box',
        inbound: 'vless-in',
        outbound: 'direct',
        user: 'alice',
        network: 'tcp',
        source: '203.0.113.9:54321',
        sourceIp: '203.0.113.9',
        destination: '93.184.216.34:443',
        domain: 'example.com',
        protocol: 'tls',
        startedAt: '2026-09-23T10:59:00Z',
        upload: 1024,
        download: 2048,
      }],
    }),
  }))

  await page.goto('/app/sessions')
  await expect(page.getByRole('heading', { name: '实时连接' })).toBeVisible()
  await expect(page.getByText('203.0.113.9').first()).toBeVisible()

  const search = page.getByRole('textbox', { name: '搜索用户、来源 IP、目标地址、入站或出站' })
  await search.fill('203.0.113.9')
  await expect(page.getByText('alice').first()).toBeVisible()
  await expect(page.getByText('example.com').first()).toBeVisible()
})
