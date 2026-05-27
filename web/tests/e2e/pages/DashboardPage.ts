import { Page, expect } from '@playwright/test'
import { BasePage } from './BasePage'

export class DashboardPage extends BasePage {
  constructor(page: Page) { super(page) }
  async goto() { await this.navigate('/') }
  async expectStatCardsVisible() {
    await expect(this.page.locator('[data-testid="stat-total-tasks"]')).toBeVisible()
    await expect(this.page.locator('[data-testid="stat-enabled"]')).toBeVisible()
    await expect(this.page.locator('[data-testid="stat-today-runs"]')).toBeVisible()
    await expect(this.page.locator('[data-testid="stat-failures"]')).toBeVisible()
  }
  async expectSuccessRateVisible() {
    await expect(this.page.locator('[data-testid="success-rate"]')).toBeVisible()
  }
}
