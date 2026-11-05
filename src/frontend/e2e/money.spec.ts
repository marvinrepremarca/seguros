import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

// Fase 3: la póliza expedida genera cartera y caso de renovación (por el worker); Cartera registra un
// pago. Requiere deploy/scripts/e2e-full.sh. El tenant de prueba tiene plan profesional, sin Comisiones.
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

/** Expide una póliza por la UI y devuelve su id. */
async function issuePolicy(page: Page, insurerName: string, lineName: string, suffix: string) {
  const holderName = `Luz Rojas ${suffix}`;
  const cc = `2${String(Date.now()).slice(-9)}`;
  await goTo(page, "Clientes");
  await page.getByRole("link", { name: "Nuevo cliente" }).click();
  await page.getByLabel("Nombre o razón social").fill(holderName);
  await page.getByLabel("Número de documento").fill(cc);
  await page.getByRole("button", { name: "Guardar cliente" }).click();
  await expect(page.getByRole("heading", { level: 1, name: holderName })).toBeVisible();

  await goTo(page, "Pólizas");
  await page.getByRole("link", { name: "Nueva póliza" }).click();
  await page.getByLabel("Aseguradora", { exact: true }).selectOption({ label: insurerName });
  await page.getByLabel("Ramo", { exact: true }).selectOption({ label: lineName });
  const option = `${holderName} (CC •••• ${cc.slice(-4)})`;
  await page.getByLabel("Buscar tomador").fill(holderName);
  await expect(
    page.getByLabel("Tomador", { exact: true }).locator("option", { hasText: holderName }),
  ).toBeAttached();
  await page.getByLabel("Tomador", { exact: true }).selectOption({ label: option });
  await page.getByLabel("Buscar cliente 1").fill(holderName);
  await expect(
    page.getByLabel("Cliente 1", { exact: true }).locator("option", { hasText: holderName }),
  ).toBeAttached();
  await page.getByLabel("Cliente 1", { exact: true }).selectOption({ label: option });
  await page.getByLabel("Prima neta").fill("1000000");
  await page.getByLabel("Impuestos y gastos").fill("190000");
  await page.getByLabel("Vigente desde").fill("2026-01-01");
  await page.getByLabel("Vigente hasta").fill("2030-12-31");
  await page.getByLabel("Cobertura 1").fill("Daños a terceros");
  await page.getByLabel("Suma asegurada 1").fill("50000000");
  await page.getByLabel("Descripción del bien 1").fill("Mazda 3");
  await page.getByLabel("Placa *").fill("ABC123");
  await page.getByLabel("Marca *").fill("Mazda");
  await page.getByLabel("Modelo *").fill("3");
  await page.getByLabel("Año *").fill("2024");
  await page.getByRole("button", { name: "Guardar póliza" }).click();
  await page.getByRole("button", { name: "El cliente aceptó" }).click();
  await page.getByRole("button", { name: "Registrar expedición" }).click();
  await page.getByLabel("Número de póliza").fill(`POL-${suffix}`);
  await page.getByRole("button", { name: "Expedir" }).click();
  await expect(page.getByText("Vigente", { exact: true })).toBeVisible();
  const id = /polizas\/([0-9a-f-]{36})/.exec(page.url())?.[1];
  if (!id) throw new Error("sin id de póliza en la URL");
  return id;
}

test("la póliza genera cartera, se registra un pago y se gestiona su renovación", async ({
  browser,
}) => {
  test.setTimeout(150_000);
  const suffix = String(Date.now()).slice(-8);
  const insurerName = `Aseguradora M${suffix}`;
  const lineName = `Autos M${suffix}`;

  const adminContext = await browser.newContext();
  const admin = await adminContext.newPage();
  await login(admin, "admin.dev");
  await goTo(admin, "Catálogo");
  await admin.getByLabel("Nueva aseguradora").fill(insurerName);
  await admin.getByRole("button", { name: "Agregar aseguradora" }).click();
  await expect(admin.getByText(insurerName).first()).toBeVisible();
  await admin.getByLabel("Nuevo ramo").fill(lineName);
  await admin.getByLabel("Datos de riesgo").selectOption({ label: "Autos" });
  await admin.getByRole("button", { name: "Agregar ramo" }).click();
  await expect(admin.getByText(lineName).first()).toBeVisible();
  await adminContext.close();

  const opsContext = await browser.newContext();
  const ops = await opsContext.newPage();
  await login(ops, "ops.dev");
  const policyId = await issuePolicy(ops, insurerName, lineName, suffix);

  // Cartera: el plan lo crea el worker al consumir PolicyIssued.
  const collectionsContext = await browser.newContext();
  const collections = await collectionsContext.newPage();
  await login(collections, "collections.dev");
  await goTo(collections, "Cartera y cobro");
  await expect(
    collections.getByRole("heading", { level: 1, name: "Cartera y cobro" }),
  ).toBeVisible();
  await expectAccessible(collections);
  await expect(async () => {
    await collections.goto(`/cartera/${policyId}`);
    await expect(
      collections.getByRole("heading", { level: 1, name: new RegExp(`POL-${suffix}`) }),
    ).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 30_000 });
  await collections.getByLabel("Valor pagado").fill("1190000");
  await collections.getByLabel("Fecha de pago").fill("2026-02-01");
  await collections.getByLabel("Referencia del soporte").fill("REC-1");
  await collections.getByRole("button", { name: "Registrar pago informado" }).click();
  await expect(collections.getByRole("cell", { name: "Pagada" })).toBeVisible();
  await expectAccessible(collections);
  await collectionsContext.close();

  // Renovaciones: el caso aparece y se inicia la gestión.
  await goTo(ops, "Renovaciones");
  await expect(ops.getByRole("heading", { level: 1, name: "Renovaciones" })).toBeVisible();
  await expectAccessible(ops);
  await ops.goto(`/polizas/${policyId}`);
  await expect(ops.getByRole("link", { name: "Registrar renovación" })).toBeVisible();
  await opsContext.close();
});
