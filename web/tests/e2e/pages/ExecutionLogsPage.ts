import { Page, expect } from '@playwright/test'
import { BasePage } from './BasePage'

export class ExecutionLogsPage extends BasePage {
  constructor(page: Page) { super(page) }
  async goto() { await this.navigate('/logs') }
  async filterByStatus(status: string) { await this.page.selectOption('[data-testid="log-status-filter"]', status) }
  async clickExportCSV() { await this.page.click('text=Export CSV') }
  async clickExportJSON() { await this.page.click('text=Export JSON') }
}
