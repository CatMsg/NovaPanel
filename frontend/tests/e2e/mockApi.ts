import type { Page, Route } from '@playwright/test'

export const json = (obj: unknown, msg = '', success = true) =>
  JSON.stringify({ success, msg, obj })

export const installBaseMocks = async (page: Page, authenticated = true) => {
  await page.addInitScript(() => {
    localStorage.setItem('locale', 'zhHans')
    localStorage.setItem('theme', 'light')
  })
  await page.route('**/api/**', async (route: Route) => {
    const url = new URL(route.request().url())
    const action = url.pathname.split('/').pop() ?? ''
    if (action === 'checkLogin') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: json(null, '', authenticated) })
      return
    }
    if (action === 'load') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: json({ onlines: { inbound: [], outbound: [], user: [] }, lastUpdate: 1 }),
      })
      return
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: json({}) })
  })
}
