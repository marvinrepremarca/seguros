import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

// Fase 4: prospecto → cotización → póliza en trámite (por el worker) → siniestro.
// Requiere deploy/scripts/e2e-full.sh; el plan profesional del tenant de prueba incluye los tres módulos.
test.skip(process.env["E2E_FULL"] !== "1", "requiere el entorno completo (E2E_FULL=1)");
const password = process.env["E2E_PASSWORD"] ?? "";

async function login(page: Page, username: string) {
  await page.goto("/");
  await page.getByRole("link", { name: /iniciar sesión/i }).click();
  await page.locator("input[name=username]").fill(username);
  await page.locator("#kc-login").click();
  await page.locator("input[name=password]").fill(password);
  await page.locator("#kc-login").click();
  await expect(page.getByRole("navigation", { name: "Menú principal" })).toBeVisible();
}

async function goTo(page: Page, link: string) {
  await page
    .getByRole("navigation", { name: "Menú principal" })
    .getByRole("link", { name: link })
    .click();
}

async function expectAccessible(page: Page) {
  const axe = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(axe.violations).toEqual([]);
}

test("el asesor registra y gestiona un prospecto", async ({ page }) => {
  await login(page, "sales.dev");
  await goTo(page, "Prospectos (CRM)");
  const name = `Prospecto ${String(Date.now())}`;
  await page.getByLabel("Nombre").fill(name);
  await page.getByLabel("Teléfono").fill("3001234567");
  await page.getByRole("button", { name: "Guardar prospecto" }).click();
  const row = page.getByRole("listitem").filter({ hasText: name });
  await expect(row).toContainText("Nuevo");
  await row.getByRole("button", { name: "Marcar contactado" }).click();
  await expect(row).toContainText("Contactado");
  await row.getByRole("button", { name: "Marcar interesado" }).click();
  await expect(row).toContainText("Interesado");
  await expectAccessible(page);
});

test("cotizaciones y siniestros abren para el asesor sin datos de salud", async ({ page }) => {
  await login(page, "sales.dev");
  await goTo(page, "Cotizaciones");
  await expect(page.getByRole("heading", { level: 1, name: "Cotizaciones" })).toBeVisible();
  await expectAccessible(page);
  await goTo(page, "Siniestros");
  await expect(page.getByRole("heading", { level: 1, name: "Siniestros" })).toBeVisible();
  await expect(page.getByText("Detalle de salud")).toHaveCount(0);
  await expectAccessible(page);
});
