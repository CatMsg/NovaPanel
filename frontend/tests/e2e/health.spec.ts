import { expect, test } from '@playwright/test'
import { installBaseMocks, json } from './mockApi'

test('health page loads diagnostics and exposes localized alert settings', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.route('**/api/health**', route => route.fulfill({
    status: 200, contentType: 'application/json',
    body: json({
      status: 'healthy', checkedAt: '2026-09-23T06:00:00Z', durationMs: 4,
      summary: { ok: 1, warning: 0, error: 0, info: 0 },
      checks: [{ id: 'database', title: '数据库', status: 'ok', summary: 'SQLite quick_check 通过' }],
      diagnostics: { ports: { drift: { issues: [] } }, loginProtection: { supported: true, installed: true, active: true, jail: 'novapanel', bannedIps: [] } },
    }),
  }))
  await page.route('**/api/alert-settings**', route => route.fulfill({
    status: 200, contentType: 'application/json',
    body: json({ enabled: true, telegramToken: '', telegramTokenSet: true, telegramChatId: '123', intervalMinutes: 5, cooldownMinutes: 60, language: 'zhHans' }),
  }))

  await page.goto('/app/health')
  await expect(page.getByRole('heading', { name: '健康与诊断' })).toBeVisible()
  await expect(page.getByText('数据库').first()).toBeVisible()
  await page.getByRole('button', { name: '配置' }).click()
  await expect(page.getByLabel('通知语言')).toBeVisible()
  await expect(page.getByLabel('Telegram Chat ID')).toHaveValue('123')
})
