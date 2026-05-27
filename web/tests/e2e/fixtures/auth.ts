import { Page } from '@playwright/test'

// Storage state (from global-setup.ts) already has a valid token in localStorage.
// login() just navigates to the dashboard — no API call needed.
export async function login(page: Page) {
  await page.goto('/', { waitUntil: 'networkidle' })
  await page.waitForSelector('[data-testid="stat-total-tasks"]', { state: 'visible', timeout: 10000 })
}

export async function logout(page: Page) {
  await page.goto('/')
  await page.click('[data-testid="nav-logout"]')
  await page.waitForSelector('[data-testid="login-submit"]', { state: 'visible', timeout: 10000 })
}
