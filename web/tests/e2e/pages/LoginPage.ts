import { Page, expect } from '@playwright/test'
import { BasePage } from './BasePage'

export class LoginPage extends BasePage {
  constructor(page: Page) { super(page) }
  async goto() { await this.navigate('/login') }
  async fillCredentials(username: string, password: string) {
    await this.page.fill('[data-testid="login-username"]', username)
    await this.page.fill('[data-testid="login-password"]', password)
  }
  async submit() { await this.page.click('[data-testid="login-submit"]') }
  async login(username: string, password: string) {
    await this.goto()
    await this.fillCredentials(username, password)
    await this.submit()
  }
  async expectErrorVisible() {
    await expect(this.page.locator('[data-testid="login-error"]')).toBeVisible()
  }
  async expectRedirectToDashboard() {
    await this.page.waitForURL('/')
  }
}
