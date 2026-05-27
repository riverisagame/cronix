import { Page, expect } from '@playwright/test'

export class BasePage {
  constructor(protected page: Page) {}

  async navigate(path: string) {
    await this.page.goto(path)
  }

  async expectUrlContains(path: string) {
    await expect(this.page).toHaveURL(new RegExp(path))
  }
}
