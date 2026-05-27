import { test, expect } from '@playwright/test'
import { LoginPage } from '../pages/LoginPage'

test.describe('Login', () => {
  let loginPage: LoginPage

  test.beforeEach(async ({ page }) => {
    // Navigate first to get a document origin, then clear the stored token
    await page.goto('/login', { waitUntil: 'commit' })
    await page.evaluate(() => localStorage.clear())
    loginPage = new LoginPage(page)
  })

  test('successful login redirects to dashboard', async ({ page }) => {
    await loginPage.login('admin', 'admin')
    await loginPage.expectRedirectToDashboard()
  })

  test('failed login shows error message', async ({ page }) => {
    await loginPage.goto()
    await loginPage.fillCredentials('admin', 'wrong-password')
    await loginPage.submit()
    await loginPage.expectStillOnLogin()
  })

  test('empty fields show validation or error', async ({ page }) => {
    await loginPage.goto()
    await loginPage.submit()
    await expect(page.locator('[data-testid="login-error"]').or(page.locator('input:invalid'))).toBeAttached()
  })
})
