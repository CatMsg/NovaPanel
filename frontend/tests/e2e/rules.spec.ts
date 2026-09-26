import { expect, test } from '@playwright/test'
import { installBaseMocks, json } from './mockApi'

test('rule editor exposes quick match conditions and optional domain fields', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/load**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json({
      config: { route: { rules: [], rule_set: [] }, dns: { servers: [], rules: [] } },
      lastUpdate: 1,
      onlines: { inbound: [], outbound: [], user: [] },
      inbounds: [],
      outbounds: [{ id: 1, type: 'direct', tag: 'direct' }],
      clients: [],
      endpoints: [],
      services: [],
      tls: [],
    }),
  }))
  await page.route('**/api/ruleset-health**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: json([]),
  }))

  await page.goto('/app/rules')
  await page.getByRole('button', { name: '手动添加' }).click()

  const editor = page.getByRole('dialog').last()
  await expect(editor.getByText('匹配条件', { exact: true })).toBeVisible()
  const domainCondition = editor.getByRole('button', { name: '域名/IP' })
  await domainCondition.click()
  await expect(domainCondition).toHaveAttribute('aria-pressed', 'true')
  await expect(editor.getByRole('textbox', { name: '域名', exact: true })).toBeVisible()
  await editor.getByRole('button', { name: '关闭' }).click()
  await expect(editor).toBeHidden()
})
