import { test, expect } from '@playwright/test'
import { login } from '../fixtures/auth'
import { GroupListPage } from '../pages/GroupListPage'
import { GroupEditPage } from '../pages/GroupEditPage'

test.describe('Groups', () => {
  test.beforeEach(async ({ page }) => {
    await login(page)
  })

  test('group list loads', async ({ page }) => {
    const groupList = new GroupListPage(page)
    await groupList.goto()
    await expect(page.locator('body')).toBeVisible()
  })

  test('new group button navigates', async ({ page }) => {
    const groupList = new GroupListPage(page)
    await groupList.goto()
    await groupList.clickNewGroup()
    await expect(page).toHaveURL(/\/groups\/new/)
  })

  test('create group form renders', async ({ page }) => {
    const groupEdit = new GroupEditPage(page)
    await groupEdit.gotoNew()
    await expect(page.locator('[data-testid="group-form-name"]')).toBeVisible()
    await expect(page.locator('[data-testid="group-form-mode"]')).toBeVisible()
    await expect(page.locator('[data-testid="btn-save-group"]')).toBeVisible()
  })

  test('create group saves and redirects', async ({ page }) => {
    const groupEdit = new GroupEditPage(page)
    await groupEdit.gotoNew()
    await groupEdit.fillName('e2e-test-group')
    await groupEdit.clickSave()
    await groupEdit.expectSaved()
  })
})
