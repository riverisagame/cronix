import { Page, expect } from '@playwright/test'
import { BasePage } from './BasePage'

export class TaskEditPage extends BasePage {
  constructor(page: Page) { super(page) }
  async gotoNew() { await this.navigate('/tasks/new') }
  async gotoEdit(id: number) { await this.navigate(`/tasks/${id}`) }
  async fillForm(name: string, command: string, cron: string) {
    await this.page.fill('[data-testid="task-form-name"]', name)
    await this.page.fill('[data-testid="task-form-command"]', command)
    await this.page.fill('[data-testid="task-form-cron"]', cron)
  }
  async clickSave() { await this.page.click('[data-testid="btn-save-task"]') }
  async expectRedirectToList() { await this.page.waitForURL('/tasks') }
}
