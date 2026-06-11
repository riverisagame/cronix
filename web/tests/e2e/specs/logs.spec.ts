import { test, expect } from '@playwright/test'
import { login } from '../fixtures/auth'
import { ExecutionLogsPage } from '../pages/ExecutionLogsPage'

test.describe('Execution Logs', () => {
  test.beforeEach(async ({ page }) => {
    await login(page)
  })

  test('log list loads', async ({ page }) => {
    const logsPage = new ExecutionLogsPage(page)
    await logsPage.goto()
    await expect(page.locator('body')).toBeVisible()
  })

  test('status filter is visible', async ({ page }) => {
    const logsPage = new ExecutionLogsPage(page)
    await logsPage.goto()
    await logsPage.expectStatusFilterVisible()
  })

  test('export buttons exist', async ({ page }) => {
    const logsPage = new ExecutionLogsPage(page)
    await logsPage.goto()
    await expect(page.locator('text=Export CSV')).toBeVisible()
    await expect(page.locator('text=Export JSON')).toBeVisible()
  })

  test('CSV export triggers download', async ({ page }) => {
    const logsPage = new ExecutionLogsPage(page)
    await logsPage.goto()
    const [download] = await Promise.all([
      page.waitForEvent('download', { timeout: 10000 }),
      logsPage.clickExportCSV(),
    ])
    expect(download.suggestedFilename()).toContain('csv')
  })

  test('clicking a log opens unified LogViewer with fullscreen option', async ({ page }) => {
    // First run a task to ensure we have a log
    await page.goto('/tasks')
    await page.waitForSelector('[data-testid="btn-run-task"]', { state: 'visible', timeout: 10000 })
    await page.click('[data-testid="btn-run-task"]')
    await page.waitForTimeout(1000)
    await page.locator('.el-drawer__close-btn').click()
    await expect(page.locator('.el-drawer')).toBeHidden({ timeout: 5000 })

    const logsPage = new ExecutionLogsPage(page)
    await logsPage.goto()
    // Wait for data
    await page.waitForTimeout(1000)
    // Click the first log row
    const firstRow = page.locator('.el-table-v2__row').first()
    await expect(firstRow).toBeVisible()
    await firstRow.click()
    
    // Expect the unified LogViewer to appear with terminal-body and fullscreen button
    await expect(page.locator('.terminal-body')).toBeVisible({ timeout: 5000 })
    await expect(page.locator('[data-testid="btn-fullscreen"]')).toBeVisible()
  })
})
