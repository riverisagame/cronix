import { Page } from '@playwright/test'
import { BasePage } from './BasePage'

export class SettingsPage extends BasePage {
  constructor(page: Page) { super(page) }
  async goto() { await this.navigate('/settings') }
  async clickSave() { await this.page.click('[data-testid="btn-save-settings"]') }
}
