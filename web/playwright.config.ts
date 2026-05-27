import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: 'tests/e2e/specs',
  outputDir: 'tests/e2e/results',
  fullyParallel: false,
  retries: 0,
  workers: 1,
  reporter: [['list'], ['html', { outputFolder: 'tests/e2e/report' }]],
  timeout: 30000,
  globalSetup: 'tests/e2e/global-setup.ts',
  use: {
    baseURL: 'http://localhost:8080',
    browserName: 'chromium',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        storageState: 'tests/e2e/storage-state.json',
      },
    },
  ],
  webServer: {
    command: 'echo "Server must be started externally (Go binary)"',
    url: 'http://localhost:8080/api/health',
    reuseExistingServer: true,
  },
})
