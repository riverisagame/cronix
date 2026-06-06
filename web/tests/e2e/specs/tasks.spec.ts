import { test, expect } from '@playwright/test'
import { login } from '../fixtures/auth'
import { TaskListPage } from '../pages/TaskListPage'

test.describe('Tasks', () => {
  test.beforeEach(async ({ page }) => {
    await login(page)
  })

  test('task list loads with data', async ({ page }) => {
    const taskList = new TaskListPage(page)
    await taskList.goto()
    await taskList.expectTableVisible()
  })

  test('search filters tasks', async ({ page }) => {
    const taskList = new TaskListPage(page)
    await taskList.goto()
    await taskList.searchTasks('backup')
    await page.waitForTimeout(500)
    await taskList.expectTableVisible()
  })

  test('new task button navigates to create page', async ({ page }) => {
    const taskList = new TaskListPage(page)
    await taskList.goto()
    await taskList.clickNewTask()
    await expect(page).toHaveURL(/\/tasks\/new/)
  })

  test('create task saves and redirects to list', async ({ page }) => {
    await page.goto('/tasks/new')
    await page.waitForSelector('[data-testid="task-form-name"]', { state: 'visible' })
    const uniqueName = `e2e-test-backup-${Date.now()}`
    await page.fill('[data-testid="task-form-name"]', uniqueName)
    await page.fill('[data-testid="task-form-command"]', 'echo e2e-test')
    await page.click('[data-testid="btn-save-task"]')
    await expect(page).toHaveURL(/\/tasks/)
    await page.waitForSelector('[data-testid="task-table"]')
  })

  test('run task triggers execution', async ({ page }) => {
    const taskList = new TaskListPage(page)
    await taskList.goto()
    await page.waitForTimeout(500)
    await taskList.clickRunTask()
    // Should not crash on rapid double-click
    await taskList.clickRunTask()
    await page.waitForTimeout(500)
  })

  test('log drawer opens', async ({ page }) => {
    const taskList = new TaskListPage(page)
    await taskList.goto()
    await page.waitForTimeout(500)
    await taskList.clickTaskLogs()
    await page.waitForTimeout(500)
  })
})
