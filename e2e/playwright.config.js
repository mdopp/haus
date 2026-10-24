// Haus in a real browser (owner, 2026-10-24: testing and UX both matter).
// cmd/hausdemo serves the family pages against fakes; one worker, as the
// demo's house is shared. Phone first: a Pixel-sized viewport, dark.
const { defineConfig, devices } = require('@playwright/test');

module.exports = defineConfig({
  testDir: '.',
  workers: 1,
  timeout: 60_000,
  retries: 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    ...devices['Pixel 7'],
    baseURL: 'http://127.0.0.1:8651',
    colorScheme: 'dark',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command: 'go run ../cmd/hausdemo -addr 127.0.0.1:8651',
    url: 'http://127.0.0.1:8651/start',
    timeout: 180_000,
    reuseExistingServer: !process.env.CI,
  },
});
