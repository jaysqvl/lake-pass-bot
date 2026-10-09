import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 90_000,
  use: { baseURL: 'http://127.0.0.1:18092', ...devices['Desktop Chrome'], actionTimeout: 10_000, trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  webServer: {
    command: 'go run ../../scripts/ui-fixture',
    url: 'http://127.0.0.1:18092/healthz',
    reuseExistingServer: false,
    timeout: 120_000,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 5_000 },
  },
})
