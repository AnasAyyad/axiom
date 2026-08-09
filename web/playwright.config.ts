import { defineConfig, devices } from "@playwright/test";

const integratedBaseURL =
  process.env.AXIOM_OWNER_CONSOLE_E2E_BASE_URL?.trim() || undefined;

export default defineConfig({
  testDir: "./tests/e2e",
  timeout: 30_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  reporter: "line",
  testMatch:
    integratedBaseURL === undefined
      ? "owner-console-workflow.spec.ts"
      : "owner-console-integrated-workflow.spec.ts",
  use: {
    baseURL: integratedBaseURL ?? "http://127.0.0.1:4173",
    trace: "retain-on-failure",
  },
  projects:
    integratedBaseURL === undefined
      ? [
          {
            name: "chromium-desktop",
            use: { ...devices["Desktop Chrome"] },
          },
          {
            name: "firefox-desktop",
            use: { ...devices["Desktop Firefox"] },
          },
          {
            name: "webkit-desktop",
            use: { ...devices["Desktop Safari"] },
          },
          {
            name: "chromium-tablet",
            use: { ...devices["iPad Pro 11"], browserName: "chromium" },
          },
          { name: "chromium-mobile", use: { ...devices["Pixel 7"] } },
        ]
      : [
          {
            // The integrated environment is deliberately stateful: a single
            // browser owns the one permitted active shadow session. Responsive
            // coverage remains in the deterministic fixture project above.
            name: "chromium-integrated",
            use: { ...devices["Desktop Chrome"] },
          },
        ],
  webServer:
    integratedBaseURL === undefined
      ? {
          command:
            "node_modules/.bin/tsc -b --pretty false && node_modules/.bin/vite build && node_modules/.bin/vite preview --host 127.0.0.1 --port 4173",
          url: "http://127.0.0.1:4173",
          reuseExistingServer: false,
          timeout: 120_000,
        }
      : undefined,
});
