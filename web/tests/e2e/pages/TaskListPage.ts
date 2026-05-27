import { Page, expect } from '@playwright/test'
import { BasePage } from './BasePage'

export class TaskListPage extends BasePage {
  constructor(page: Page) { super(page) }
  async goto() { await this.navigate('/tasks') }
  async clickNewTask() { await this.page.click('[data-testid="btn-new-task"]') }
  async searchTasks(query: string) { await this.page.fill('[data-testid="task-search"]', query); await this.page.keyboard.press('Enter') }
  async clickRunTask() { await this.page.click('[data-testid="btn-run-task"]') }
  async clickTaskLogs() { await this.page.click('[data-testid="btn-task-logs"]') }
  async expectTableVisible() { await expect(this.page.locator('[data-testid="task-table"]')).toBeVisible() }
}
