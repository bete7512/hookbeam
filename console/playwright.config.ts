import { defineConfig, devices } from "@playwright/test";

// The suite runs against the local stack behind the proxy: the console (dev or start), the API, the data plane
// and Mailpit. CONSOLE_URL and MAILPIT_URL override the defaults.
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  retries: 0,
  reporter: [["list"]],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL: process.env.CONSOLE_URL ?? "http://localhost:3000",
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
