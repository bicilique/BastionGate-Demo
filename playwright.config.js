import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/e2e",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: "line",
  use: {
    baseURL: "http://127.0.0.1:13001",
    browserName: "chromium",
    headless: true,
    viewport: { width: 1440, height: 810 },
  },
  webServer: {
    command: "go run ./internal/e2eharness",
    url: "http://127.0.0.1:13001/api/health",
    reuseExistingServer: false,
    timeout: 30_000,
  },
});

