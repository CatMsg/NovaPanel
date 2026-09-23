import { expect, test } from '@playwright/test'

test('unauthenticated app renders the login screen', async ({ page }) => {
  await page.route('**/api/checkLogin**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ success: false, msg: '', obj: null }),
    })
  })

  await page.goto('/app/')
  await expect(page).toHaveURL(/\/app\/login$/)
  await expect(page.getByText('NovaPanel').first()).toBeVisible()
  await expect(page.locator('input[type="password"]')).toBeVisible()
  await expect(page.locator('form button[type="submit"]')).toBeVisible()
})
