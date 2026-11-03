import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

// No necesita backend: la página de login es pública.
test("la página de inicio de sesión carga y cumple WCAG 2.2 AA", async ({ page }) => {
  await page.goto("/login");
  await expect(page.getByRole("heading", { level: 1, name: "Seguros ERP" })).toBeVisible();
  await expect(page.getByRole("link", { name: /iniciar sesión/i })).toHaveAttribute(
    "href",
    "/bff/auth/login?returnTo=%2F",
  );

  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(results.violations).toEqual([]);
});
