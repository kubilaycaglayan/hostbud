import { defineConfig, devices } from '@playwright/test'

// Runs inside hostbud-e2e-runner, which shares hostbud-e2e-caddy's network
// namespace: http://localhost:9055 is the real Caddy loopback site.
export default defineConfig({
  testDir: './tests',
  outputDir: './results/artifacts',
  globalSetup: './global-setup.ts',
  // One shared target: run serially so scenarios never see each other's sessions.
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  timeout: 30_000,
  expect: { timeout: 5_000 },
  reporter: [['list'], ['html', { outputFolder: 'results/report', open: 'never' }]],
  use: {
    baseURL: 'http://localhost:9055',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    // Signed in as the account global setup creates (helpers/auth.ts).
    storageState: 'results/.auth.json',
  },
  projects: [
    { name: 'desktop-chromium', use: { ...devices['Desktop Chrome'] } },
    {
      name: 'iphone-13-pro',
      use: { ...devices['iPhone 13 Pro'] },
      // API-level scenarios (*.api.spec.ts) run in the desktop project only.
      testIgnore: /\.api\.spec\.ts$/,
    },
  ],
})
