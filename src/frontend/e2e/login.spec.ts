import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

// Prueba de humo de punta a punta (ROADMAP 1.12): login OIDC real contra Keycloak a través del
// BFF y menú filtrado por los flags del tenant de prueba "agencia-demo" (plan profesional).
// Requiere el entorno completo: deploy/scripts/e2e-full.sh (o E2E_FULL=1 con la API corriendo).
const full = process.env["E2E_FULL"] === "1";
const password = process.env["E2E_PASSWORD"] ?? "";

test.skip(!full, "requiere el entorno completo (E2E_FULL=1)");

async function login(page: Page, username: string) {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login\?returnTo=/);
  await page.getByRole("link", { name: /iniciar sesión/i }).click();
  // Keycloak (Organizations habilitado) pide primero el usuario y luego la contraseña.
  await page.locator("input[name=username]").fill(username);
  await page.locator("#kc-login").click();
  await page.locator("input[name=password]").fill(password);
  await page.locator("#kc-login").click();
  await expect(page.getByRole("navigation", { name: "Menú principal" })).toBeVisible();
}

test("login y menú filtrado por los flags del plan profesional", async ({ page, context }) => {
  await login(page, "manager.dev");
  const nav = page.getByRole("navigation", { name: "Menú principal" });

  // Plan profesional: incluidos...
  for (const label of [
    "Cartera y cobro",
    "Renovaciones",
    "Siniestros",
    "Prospectos (CRM)",
    "Cotizaciones",
  ]) {
    await expect(nav.getByRole("link", { name: label })).toBeVisible();
  }
  // ...y fuera del plan (empresarial).
  for (const label of ["Comisiones", "Cumplimiento", "Reportes", "Integraciones"]) {
    await expect(nav.getByRole("link", { name: label })).toHaveCount(0);
  }
  // Manager tiene featuremgmt.flag.read.
  await expect(nav.getByRole("link", { name: "Funcionalidades" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 1, name: /Bienvenido/ })).toBeVisible();

  // La sesión es una cookie HttpOnly __Host-: el JS de la página no la ve.
  const cookies = await context.cookies();
  const session = cookies.find((c) => c.name === "__Host-seguros_session");
  expect(session?.httpOnly).toBe(true);
  expect(session?.sameSite).toBe("Strict");
  expect(await page.evaluate(() => document.cookie)).not.toContain("seguros_session");

  // Un módulo fuera del plan responde como inexistente aunque se escriba la URL.
  await page.goto("/comisiones");
  await expect(page.getByRole("heading", { name: "Página no encontrada" })).toBeVisible();
  await page.goto("/notificaciones");
  await expect(page.getByRole("heading", { level: 1, name: "Notificaciones" })).toBeVisible();

  const axe = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(axe.violations).toEqual([]);

  await page.getByRole("button", { name: "Cerrar sesión" }).click();
  await page.waitForURL(/realms\/seguros-dev\/protocol\/openid-connect\/logout|\/login/);
  const after = await context.request.get("/bff/me");
  expect(after.status()).toBe(401);
});

test("un rol sin permisos de configuración no ve Funcionalidades", async ({ page }) => {
  await login(page, "sales.dev");
  const nav = page.getByRole("navigation", { name: "Menú principal" });
  await expect(nav.getByRole("link", { name: "Siniestros" })).toBeVisible();
  await expect(nav.getByRole("link", { name: "Funcionalidades" })).toHaveCount(0);
});
