import { expect, test } from '@playwright/test'
import { installBaseMocks } from './mockApi'

test('command palette opens by shortcut and navigates to a filtered page', async ({ page }) => {
  await installBaseMocks(page, true)
  await page.goto('/app/')

  const dialog = page.getByRole('dialog')
  await page.getByRole('button', { name: '快速跳转' }).click()
  await expect(dialog).toBeVisible()
  await page.keyboard.press('Escape')
  await page.keyboard.press('Control+k')
  await expect(dialog).toBeVisible()
  const search = dialog.getByRole('textbox')
  await search.fill('DNS')
  const dnsLink = dialog.getByRole('link', { name: 'DNS 网络与规则', exact: true })
  await expect(dnsLink).toBeVisible()
  await search.press('Enter')

  await expect(page).toHaveURL(/\/app\/dns$/)
  await expect(page.getByRole('heading', { name: 'DNS', exact: true })).toBeVisible()
})
