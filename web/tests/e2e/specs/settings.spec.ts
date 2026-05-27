import { test, expect } from '@playwright/test'
import { login } from '../fixtures/auth'
import { SettingsPage } from '../pages/SettingsPage'

test.describe('Settings', () => {
  test.beforeEach(async ({ page }) => {
    await login(page)
  })

  test('settings page loads', async ({ page }) => {
    const settingsPage = new SettingsPage(page)
    await settingsPage.goto()
    await expect(page.locator('[data-testid="btn-save-settings"]')).toBeVisible()
  })

  test('can save settings', async ({ page }) => {
    const settingsPage = new SettingsPage(page)
    await settingsPage.goto()
    await settingsPage.clickSave()
    await page.waitForTimeout(500)
  })
})
