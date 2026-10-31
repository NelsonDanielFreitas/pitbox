import path from "node:path";

import { defineConfig, devices } from "@playwright/test";

// F1-14: the browser against the real pitbox-api (sync, signatures, RLS) over throwaway
// containers. `pnpm e2e` keeps using the mock BFF; this one needs Docker.
const port = 4174;
const baseURL = `http://localhost:${port}`;
const stateFile = path.resolve(import.meta.dirname, ".e2e/state.json");

export default defineConfig({
  testDir: "./tests/e2e-sync",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: "list",
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  webServer: [
    {
      command: "bash ../scripts/e2e-server.sh",
      url: "http://127.0.0.1:8089/healthz",
      reuseExistingServer: false,
      timeout: 240_000,
      // Playwright's default is SIGKILL: the containers would be left behind. SIGTERM lets the
      // test binary remove them.
      gracefulShutdown: { signal: "SIGTERM", timeout: 30_000 },
      stdout: "pipe",
      stderr: "pipe",
      env: { PITBOX_E2E_ORIGIN: baseURL, PITBOX_E2E_STATE: stateFile },
    },
    {
      command: `pnpm preview --host 127.0.0.1 --port ${port} --strictPort`,
      url: baseURL,
      reuseExistingServer: false,
      timeout: 30_000,
      env: { PITBOX_API_URL: "http://127.0.0.1:8081" },
    },
  ],
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"], viewport: { width: 390, height: 844 } } }],
});
