import { Page } from '@playwright/test'

export async function login(page: Page, username = 'admin', password = 'admin') {
  await page.goto('/login', { waitUntil: 'networkidle' })
  await page.waitForSelector('[data-testid="login-submit"]', { state: 'visible', timeout: 10000 })
  await page.fill('[data-testid="login-username"]', username)
  await page.fill('[data-testid="login-password"]', password)

  // Wait for the login API response and navigation together
  await Promise.all([
    page.waitForResponse(resp => resp.url().includes('/api/login') && resp.status() === 200, { timeout: 15000 }),
    page.click('[data-testid="login-submit"]'),
  ])

  // After login API succeeds, the SPA stores token and calls router.push('/')
  // Wait for dashboard content to appear instead of relying on URL detection
  await page.waitForSelector('[data-testid="stat-total-tasks"]', { state: 'visible', timeout: 15000 })
}

export async function logout(page: Page) {
  await page.click('[data-testid="nav-logout"]')
  await page.waitForURL('**/login', { timeout: 10000 })
}
