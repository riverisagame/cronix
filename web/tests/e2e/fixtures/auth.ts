import { Page } from '@playwright/test'

export async function login(page: Page, username = 'admin', password = 'admin') {
  await page.goto('/login', { waitUntil: 'networkidle' })
  // Wait for the Vue SPA to fully render the login form
  await page.waitForSelector('[data-testid="login-submit"]', { state: 'visible', timeout: 10000 })
  await page.fill('[data-testid="login-username"]', username)
  await page.fill('[data-testid="login-password"]', password)
  await page.click('[data-testid="login-submit"]')
  // The SPA uses router.push('/') after storing token — URL becomes /
  await page.waitForURL('**/', { timeout: 15000 })
}

export async function logout(page: Page) {
  await page.click('[data-testid="nav-logout"]')
  await page.waitForURL('**/login', { timeout: 10000 })
}
