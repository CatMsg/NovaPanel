import { expect, test } from '@playwright/test'
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
  await expect(page.getByText('浏览记录', { exact: true })).toBeVisible()
  await expect(page.getByText('流量图表', { exact: true })).toHaveCount(0)

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.locator('.clients-mobile-card').filter({ hasText: 'alice' })).toBeVisible()
  await page.getByRole('button', { name: '更多操作' }).first().click()
  await expect(page.getByText('浏览记录', { exact: true })).toBeVisible()
  await expect(page.getByText('流量图表', { exact: true })).toHaveCount(0)
})
