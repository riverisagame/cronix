import { test, expect } from '@playwright/test'
import { login } from '../fixtures/auth'
import { DashboardPage } from '../pages/DashboardPage'

test.describe('Dashboard', () => {
  test.beforeEach(async ({ page }) => {
    await login(page)
  })

  test('displays stat cards with values', async ({ page }) => {
    const dashboard = new DashboardPage(page)
    await dashboard.goto()
    await dashboard.expectStatCardsVisible()
  })

  test('shows success rate section', async ({ page }) => {
    const dashboard = new DashboardPage(page)
    await dashboard.goto()
    await dashboard.expectSuccessRateVisible()
  })
})
