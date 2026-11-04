import { expect, test } from "@playwright/test";

// La SPA compilada servida por la API (httpx.SPAHandler), con su CSP real: ningún recurso propio
// debe quedar bloqueado. Requiere E2E_SPA_CSP=1 y E2E_BASE_URL apuntando a la API (no a Vite, que
// no aplica CSP). Lo ejecuta deploy/scripts/e2e-full.sh.
test.skip(process.env["E2E_SPA_CSP"] !== "1", "requiere la SPA servida por la API (E2E_SPA_CSP=1)");

test("la CSP con nonce y Trusted Types no bloquea la aplicación", async ({ page }) => {
  const violations: string[] = [];
  page.on("console", (m) => {
    if (/content security policy|trusted type/i.test(m.text())) violations.push(m.text());
  });
  page.on("pageerror", (e) => violations.push(e.message));

  const response = await page.goto("/login");
  const csp = response?.headers()["content-security-policy"] ?? "";
  expect(csp).toContain("'strict-dynamic'");
  expect(csp).toContain("require-trusted-types-for 'script'");
  expect(csp).not.toContain("unsafe-inline");

  // El bundle corrió (React montó la ruta, cargada en un chunk lazy) y el CSS aplicó.
  await expect(page.getByRole("heading", { level: 1, name: "Seguros ERP" })).toBeVisible();
  const display = await page.locator("main.grid").evaluate((el) => getComputedStyle(el).display);
  expect(display).toBe("grid");
  expect(violations).toEqual([]);
});

test("Trusted Types bloquea asignaciones de strings a sumideros de DOM", async ({ page }) => {
  await page.goto("/login");
  await expect(page.getByRole("heading", { level: 1, name: "Seguros ERP" })).toBeVisible();

  // innerHTML con un string plano lanza por Trusted Types.
  const sinkBlocked = await page.evaluate(() => {
    try {
      document.createElement("div").innerHTML = "<b>x</b>";
      return false;
    } catch {
      return true;
    }
  });
  expect(sinkBlocked).toBe(true);
});
