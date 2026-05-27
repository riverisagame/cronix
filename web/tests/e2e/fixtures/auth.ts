import { Page } from '@playwright/test'

export async function login(page: Page, username = 'admin', password = 'admin') {
  await page.goto('/login')
  await page.fill('[data-testid="login-username"]', username)
  await page.fill('[data-testid="login-password"]', password)
  await page.click('[data-testid="login-submit"]')
  await page.waitForURL('/')
}

export async function logout(page: Page) {
  await page.click('[data-testid="nav-logout"]')
  await page.waitForURL('/login')
}
