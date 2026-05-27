import { Page } from '@playwright/test'
import { BasePage } from './BasePage'

export class GroupEditPage extends BasePage {
  constructor(page: Page) { super(page) }
  async gotoNew() { await this.navigate('/groups/new') }
  async gotoEdit(id: number) { await this.navigate(`/groups/${id}`) }
  async fillName(name: string) { await this.page.fill('[data-testid="group-form-name"]', name) }
  async clickSave() { await this.page.click('[data-testid="btn-save-group"]') }
  async expectRedirectToList() { await this.page.waitForURL('/groups') }
}
