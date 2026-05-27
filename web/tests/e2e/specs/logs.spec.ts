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
})
