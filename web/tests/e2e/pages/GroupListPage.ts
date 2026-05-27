import { Page, expect } from '@playwright/test'
import { BasePage } from './BasePage'

export class GroupListPage extends BasePage {
  constructor(page: Page) { super(page) }
  async goto() { await this.navigate('/groups') }
  async clickNewGroup() { await this.page.click('[data-testid="btn-new-group"]') }
}
