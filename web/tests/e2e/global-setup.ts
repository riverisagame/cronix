import { chromium, FullConfig } from '@playwright/test'

async function globalSetup(config: FullConfig) {
  const baseURL = config.projects[0].use.baseURL as string
  const browser = await chromium.launch()
  const page = await browser.newPage()

  await page.goto(`${baseURL}/login`, { waitUntil: 'networkidle' })
  await page.waitForSelector('[data-testid="login-submit"]', { state: 'visible', timeout: 10000 })
  await page.fill('[data-testid="login-username"]', 'admin')
  await page.fill('[data-testid="login-password"]', 'admin')
  await page.click('[data-testid="login-submit"]')

  // Wait for dashboard to confirm login succeeded
  await page.waitForSelector('[data-testid="stat-total-tasks"]', { state: 'visible', timeout: 15000 })

  // Save the authenticated state (localStorage, cookies) to a file
  await page.context().storageState({ path: 'tests/e2e/storage-state.json' })
  await browser.close()
}

export default globalSetup
