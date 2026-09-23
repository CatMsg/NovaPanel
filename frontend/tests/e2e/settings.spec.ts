import { expect, test } from '@playwright/test'
import { installBaseMocks, json } from './mockApi'

const settings = {
  webListen: '', webDomain: '', webPort: '2095', webCertFile: '', webKeyFile: '', webPath: '/app/', webURI: '',
  sessionMaxAge: '0', loginTrustedProxies: '', loginBanAllowlist: '', trafficAge: '30',
  trafficBudgetEnabled: 'false', trafficBudgetLimitBytes: '0', trafficBudgetReserveBytes: '0', trafficBudgetOffsetBytes: '0',
  trafficBudgetAccountingMode: 'tx', trafficBudgetInterface: 'auto', trafficBudgetCycleDay: '1', trafficBudgetCycleHour: '0',
  trafficBudgetWarningPercent: '80', trafficBudgetCriticalPercent: '90', timeLocation: 'Asia/Shanghai',
  subListen: '', subPort: '2096', subPath: '/sub/', subDomain: '', subCertFile: '', subKeyFile: '', subUpdates: '12',
  subEncode: 'true', subShowInfo: 'true', subURI: '', subMode: 'slave', subMasterSources: '', subJsonExt: '', subClashExt: '',
}

test('settings can be edited and saved after preflight', async ({ page }) => {
  await installBaseMocks(page, true)
  let saveCalled = false
  await page.route('**/api/settings**', route => route.fulfill({ status: 200, contentType: 'application/json', body: json(settings) }))
  await page.route('**/api/preflight**', route => route.fulfill({ status: 200, contentType: 'application/json', body: json({ changed: true, warnings: [] }) }))
  await page.route('**/api/save**', async route => {
    saveCalled = true
    await route.fulfill({ status: 200, contentType: 'application/json', body: json({ settings: { ...settings, webPort: '2995' } }, 'set') })
  })

  await page.goto('/app/settings')
  await expect(page.getByRole('heading', { name: '设置' })).toBeVisible()
  const port = page.getByLabel('端口').first()
  await port.fill('2995')
  await page.getByRole('button', { name: '保存' }).click()
  await expect.poll(() => saveCalled).toBe(true)
})
