import { defineConfig, devices } from "@playwright/test";

const baseURL = "http://localhost:4173";

export default defineConfig({
  testDir: "./tests/e2e",
  timeout: 30_000,
  expect: { timeout: 5_000 },
  fullyParallel: false,
  workers: 1,
  reporter: "list",
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  webServer: [
    {
      command: "pnpm exec tsx tests/e2e/mock-bff.ts",
      url: "http://127.0.0.1:4010/healthz",
      reuseExistingServer: !process.env.CI,
      timeout: 30_000,
      env: { PITBOX_E2E_ORIGIN: baseURL },
    },
    {
      command: "pnpm preview --host 127.0.0.1 --port 4173",
      url: baseURL,
      reuseExistingServer: !process.env.CI,
      timeout: 30_000,
      env: { PITBOX_API_URL: "http://127.0.0.1:4010" },
    },
  ],
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
