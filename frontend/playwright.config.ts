import { defineConfig } from '@playwright/test';

// Headless, Playwright's own bundled Chromium only, launched and closed by
// the test itself. Never the machine's installed browser, never chromedriver.
export default defineConfig({
  testDir: './tests',
  timeout: 30_000,
  use: {
    headless: true,
    baseURL: 'http://localhost:4300',
  },
  webServer: [
    {
      command: 'node tests/start-backend.js',
      url: 'http://localhost:8081/api/health',
      reuseExistingServer: !process.env.CI,
      timeout: 60_000,
    },
    {
      command: 'node tests/static-server.js',
      url: 'http://localhost:4300',
      reuseExistingServer: !process.env.CI,
      timeout: 30_000,
    },
  ],
});
