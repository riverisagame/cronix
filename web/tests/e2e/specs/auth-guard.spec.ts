import { test, expect } from '@playwright/test'

test.describe('Auth Guard', () => {
  test.beforeEach(async ({ page }) => {
    // Clear storage state to start unauthenticated
    await page.evaluate(() => localStorage.clear())
  })

  test('redirects to /login when accessing /tasks without auth', async ({ page }) => {
    await page.goto('/tasks')
    await expect(page).toHaveURL(/\/login/)
  })

  test('redirects to /login when accessing /settings without auth', async ({ page }) => {
    await page.goto('/settings')
    await expect(page).toHaveURL(/\/login/)
  })

  test('redirects to /login when accessing /groups without auth', async ({ page }) => {
    await page.goto('/groups')
    await expect(page).toHaveURL(/\/login/)
  })

  test('redirects to /login when accessing /logs without auth', async ({ page }) => {
    await page.goto('/logs')
    await expect(page).toHaveURL(/\/login/)
  })

  test('redirects to /login when accessing / without auth', async ({ page }) => {
    await page.goto('/')
    await expect(page).toHaveURL(/\/login/)
  })

  test('does not redirect /login when not authenticated', async ({ page }) => {
    await page.goto('/login')
    await expect(page).toHaveURL(/\/login/)
    await page.waitForTimeout(500)
    await expect(page).toHaveURL(/\/login/)
  })
})
