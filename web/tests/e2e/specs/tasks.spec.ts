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
    
    // UI now opens the drawer immediately on run. 
    // We should expect the drawer to be visible instead of double clicking.
    await expect(page.locator('.el-drawer').locator('.terminal-body')).toBeVisible()
    
    // Close the drawer by clicking the close button
    await page.locator('.el-drawer__close-btn').click()
    await expect(page.locator('.el-drawer')).toBeHidden({ timeout: 5000 })
  })

  test('log drawer opens and shows unified LogViewer', async ({ page }) => {
    const taskList = new TaskListPage(page)
    await taskList.goto()
    await page.waitForTimeout(500)
    await taskList.clickTaskLogs()
    
    // Switch to Live Console tab
    await page.locator('.el-tabs__item', { hasText: 'Live Console' }).click()
    
    // It should have the shared terminal body
    await expect(page.locator('.el-drawer').locator('.terminal-body')).toBeVisible()
    // It should have a fullscreen button
    await expect(page.locator('.el-drawer').locator('[data-testid="btn-fullscreen"]')).toBeVisible()
    await page.locator('.el-drawer__close-btn').click()
    await expect(page.locator('.el-drawer')).toBeHidden({ timeout: 5000 })
  })

  test('live console shows explicit STOPPED visual indicator', async ({ page }) => {
    const taskList = new TaskListPage(page)
    await taskList.goto()
    await page.waitForTimeout(500)
    // Create and run a task, wait for it to stop
    await taskList.clickRunTask()
    // Switch to Live Console tab
    await page.locator('.el-tabs__item', { hasText: 'Live Console' }).click()
    
    // Wait for task to finish or check stopped banner directly
    await expect(page.locator('[data-testid="live-status-stopped"]')).toBeVisible({ timeout: 10000 })
  })
})
