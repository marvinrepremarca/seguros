import { defineConfig, devices } from "@playwright/test";

const isCI = Boolean(process.env["CI"]);
const externalBaseURL = process.env["E2E_BASE_URL"];
const baseURL = externalBaseURL ?? "http://localhost:5173";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: isCI,
  retries: isCI ? 2 : 0,
  reporter: isCI ? [["html", { open: "never" }], ["github"]] : "list",
  use: {
    baseURL,
    locale: "es-CO",
    timezoneId: "America/Bogota",
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  // Con E2E_BASE_URL se prueba un entorno ya desplegado; si no, se levanta Vite.
  ...(externalBaseURL
    ? {}
    : { webServer: { command: "pnpm dev", url: baseURL, reuseExistingServer: !isCI } }),
});
