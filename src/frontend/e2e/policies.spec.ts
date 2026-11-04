import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

// Fase 2: expedición manual (Flujo 3), endoso (Flujo 5) y cancelación (Flujo 8) desde la UI, con
// login real y bajo la CSP de la SPA. Requiere deploy/scripts/e2e-full.sh.
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

function watchViolations(page: Page): string[] {
  const violations: string[] = [];
  page.on("console", (m) => {
    if (/content security policy|trusted type/i.test(m.text())) violations.push(m.text());
  });
  page.on("pageerror", (e) => violations.push(e.message));
  return violations;
}

async function expectAccessible(page: Page) {
  const axe = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(axe.violations).toEqual([]);
}

/** CC única para cada corrida: la BD de desarrollo persiste. */
function uniqueCC(): string {
  return `1${String(Date.now()).slice(-9)}`;
}

test("expedir, endosar y cancelar una póliza desde la UI", async ({ browser }) => {
  test.setTimeout(120_000);
  const suffix = String(Date.now()).slice(-8);
  const insurerName = `Aseguradora ${suffix}`;
  const lineName = `Autos ${suffix}`;

  // El administrador configura el catálogo.
  const adminContext = await browser.newContext();
  const admin = await adminContext.newPage();
  const adminViolations = watchViolations(admin);
  await login(admin, "admin.dev");
  await goTo(admin, "Catálogo");
  await admin.getByLabel("Nueva aseguradora").fill(insurerName);
  await admin.getByRole("button", { name: "Agregar aseguradora" }).click();
  await expect(admin.getByText(insurerName).first()).toBeVisible();
  await admin.getByLabel("Nuevo ramo").fill(lineName);
  await admin.getByLabel("Datos de riesgo").selectOption({ label: "Autos" });
  await admin.getByRole("button", { name: "Agregar ramo" }).click();
  await expect(admin.getByText(lineName).first()).toBeVisible();
  await expectAccessible(admin);
  expect(adminViolations).toEqual([]);
  await adminContext.close();

  // Operaciones registra el cliente.
  const context = await browser.newContext();
  const page = await context.newPage();
  const violations = watchViolations(page);
  await login(page, "ops.dev");
  const holderName = `Ana Pérez ${suffix}`;
  const cc = uniqueCC();
  await goTo(page, "Clientes");
  await page.getByRole("link", { name: "Nuevo cliente" }).click();
  await page.getByLabel("Nombre o razón social").fill(holderName);
  await page.getByLabel("Número de documento").fill(cc);
  await page.getByRole("button", { name: "Guardar cliente" }).click();
  await expect(page.getByRole("heading", { level: 1, name: holderName })).toBeVisible();

  // El documento se muestra enmascarado y revelarlo es explícito.
  await expect(page.getByText(`CC •••• ${cc.slice(-4)}`)).toBeVisible();
  await page.getByRole("button", { name: "Mostrar documento" }).click();
  await expect(page.getByText(`CC ${cc}`)).toBeVisible();
  await expectAccessible(page);

  // Expedición manual (Flujo 3).
  await goTo(page, "Pólizas");
  await page.getByRole("link", { name: "Nueva póliza" }).click();
  await page.getByLabel("Aseguradora", { exact: true }).selectOption({ label: insurerName });
  await page.getByLabel("Ramo", { exact: true }).selectOption({ label: lineName });
  await page.getByLabel("Buscar tomador").fill(holderName);
  await expect(
    page.getByLabel("Tomador", { exact: true }).locator("option", { hasText: holderName }),
  ).toBeAttached();
  await page
    .getByLabel("Tomador", { exact: true })
    .selectOption({ label: `${holderName} (CC •••• ${cc.slice(-4)})` });
  await page.getByLabel("Buscar cliente 1").fill(holderName);
  await expect(
    page.getByLabel("Cliente 1", { exact: true }).locator("option", { hasText: holderName }),
  ).toBeAttached();
  await page
    .getByLabel("Cliente 1", { exact: true })
    .selectOption({ label: `${holderName} (CC •••• ${cc.slice(-4)})` });
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
  await expectAccessible(page);
  await page.getByRole("button", { name: "Guardar póliza" }).click();

  await expect(page.getByText("En cotización")).toBeVisible();
  await page.getByRole("button", { name: "El cliente aceptó" }).click();
  await expect(page.getByText("En trámite")).toBeVisible();
  const policyNumber = `POL-${suffix}`;
  await page.getByRole("button", { name: "Registrar expedición" }).click();
  await page.getByLabel("Número de póliza").fill(policyNumber);
  await page.getByRole("button", { name: "Expedir" }).click();
  await expect(
    page.getByRole("heading", { level: 1, name: `Póliza ${policyNumber}` }),
  ).toBeVisible();
  await expect(page.getByText("Vigente", { exact: true })).toBeVisible();
  await expectAccessible(page);

  // Endoso (Flujo 5): solicitar no cambia la prima; emitir sí.
  await page.getByRole("button", { name: "Solicitar endoso" }).click();
  await page.getByLabel("Tipo de endoso").selectOption("sum_insured_increase");
  await page.getByLabel("Fecha efectiva del endoso").fill("2026-06-01");
  await page.getByLabel("Descripción de la solicitud").fill("Aumento de la suma asegurada");
  await page.getByRole("button", { name: "Registrar solicitud" }).click();
  await expect(page.getByText("Solicitado")).toBeVisible();
  await page.getByRole("button", { name: "Registrar endoso emitido" }).click();
  await page.getByLabel("Número del endoso").fill(`END-${suffix}`);
  await page.getByLabel("Variación de prima neta").fill("100000");
  await page.getByLabel("Variación de impuestos").fill("19000");
  await page.getByLabel(/^Daños a terceros \(hoy/).fill("60000000");
  await page.getByRole("button", { name: "Aplicar endoso" }).click();
  await expect(page.getByText("Emitido")).toBeVisible();
  await expect(page.getByText(/Prima neta:.*1\.100\.000/)).toBeVisible();
  await expect(page.getByText(/Daños a terceros:.*60\.000\.000/)).toBeVisible();

  // Cancelación (Flujo 8).
  await page.getByRole("button", { name: "Registrar cancelación" }).click();
  await page.getByLabel("Fecha efectiva", { exact: true }).fill("2026-07-01");
  await page.getByLabel("Prima no devengada").fill("500000");
  await page.getByRole("button", { name: "Confirmar cancelación" }).click();
  await expect(page.getByText("Cancelada", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Solicitar endoso" })).toHaveCount(0);

  // La póliza aparece en el listado.
  await goTo(page, "Pólizas");
  await expect(page.getByRole("link", { name: policyNumber })).toBeVisible();
  expect(violations).toEqual([]);
  await context.close();
});

test("un asesor no ve acciones de operación ni pólizas ajenas", async ({ page }) => {
  await login(page, "sales.dev");
  await goTo(page, "Pólizas");
  await expect(page.getByRole("heading", { level: 1, name: "Pólizas" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Nueva póliza" })).toHaveCount(0);
  await expect(page.getByText("No hay pólizas para mostrar.")).toBeVisible();
});

test("un asesor solo ve los clientes que registró", async ({ browser }) => {
  const suffix = String(Date.now()).slice(-8);
  const register = async (page: Page, name: string, cc: string) => {
    await goTo(page, "Clientes");
    await page.getByRole("link", { name: "Nuevo cliente" }).click();
    await page.getByLabel("Nombre o razón social").fill(name);
    await page.getByLabel("Número de documento").fill(cc);
    await page.getByRole("button", { name: "Guardar cliente" }).click();
  };

  const opsContext = await browser.newContext();
  const ops = await opsContext.newPage();
  await login(ops, "ops.dev");
  const foreign = `Ajeno ${suffix}`;
  const foreignCC = `2${suffix}1`;
  await register(ops, foreign, foreignCC);
  await expect(ops.getByRole("heading", { level: 1, name: foreign })).toBeVisible();
  await opsContext.close();

  const context = await browser.newContext();
  const page = await context.newPage();
  await login(page, "sales.dev");
  const own = `Propio ${suffix}`;
  await register(page, own, `3${suffix}1`);
  await expect(page.getByRole("heading", { level: 1, name: own })).toBeVisible();
  await expect(page.getByRole("button", { name: "Mostrar documento" })).toHaveCount(0);

  await goTo(page, "Clientes");
  await expect(page.getByRole("link", { name: own })).toBeVisible();
  await expect(page.getByRole("link", { name: foreign })).toHaveCount(0);

  // El documento de otro asesor: aviso sin datos del cliente.
  await register(page, `Duplicado ${suffix}`, foreignCC);
  await expect(page.getByRole("alert")).toContainText("Ya existe un cliente con ese documento");
  await expect(page.getByText(foreign)).toHaveCount(0);
  await context.close();
});
