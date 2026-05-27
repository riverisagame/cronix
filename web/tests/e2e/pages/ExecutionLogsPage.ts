import { Page, expect } from '@playwright/test'
import { BasePage } from './BasePage'

export class ExecutionLogsPage extends BasePage {
  constructor(page: Page) { super(page) }
  async goto() { await this.navigate('/logs') }
  async expectStatusFilterVisible() {
    await expect(this.page.locator('[data-testid="log-status-filter"]')).toBeVisible({ timeout: 10000 })
  }
  async clickExportCSV() { await this.page.click('text=Export CSV') }
  async clickExportJSON() { await this.page.click('text=Export JSON') }
}
