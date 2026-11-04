import { expect, test, type APIRequestContext, type Page } from "@playwright/test";

// Una baja en Keycloak corta el acceso sin esperar a que termine la sesión. e2e-full.sh fija la
// revalidación en 3 s (en producción, 5 min).
test.skip(process.env["E2E_FULL"] !== "1", "requiere el entorno completo (E2E_FULL=1)");
const password = process.env["E2E_PASSWORD"] ?? "";
const keycloak = "http://localhost:8081";

async function adminToken(request: APIRequestContext): Promise<string> {
  const res = await request.post(`${keycloak}/realms/master/protocol/openid-connect/token`, {
    form: {
      grant_type: "password",
      client_id: "admin-cli",
      username: "admin",
      password: process.env["E2E_KC_ADMIN_PASSWORD"] ?? "",
    },
  });
  expect(res.ok()).toBe(true);
  const body: unknown = await res.json();
  if (typeof body !== "object" || body === null || !("access_token" in body)) {
    throw new Error("sin token de administración");
  }
  return String(body.access_token);
}

async function setEnabled(request: APIRequestContext, username: string, enabled: boolean) {
  const headers = { Authorization: `Bearer ${await adminToken(request)}` };
  const base = `${keycloak}/admin/realms/seguros-dev/users`;
  const found = await request.get(`${base}?username=${username}&exact=true`, { headers });
  const users: unknown = await found.json();
  const user: unknown = Array.isArray(users) ? users[0] : undefined;
  if (typeof user !== "object" || user === null || !("id" in user)) {
    throw new Error("usuario no encontrado");
  }
  const res = await request.put(`${base}/${String(user.id)}`, { headers, data: { enabled } });
  expect(res.ok()).toBe(true);
}

async function login(page: Page, username: string) {
  await page.goto("/");
  await page.getByRole("link", { name: /iniciar sesión/i }).click();
  await page.locator("input[name=username]").fill(username);
  await page.locator("#kc-login").click();
  await page.locator("input[name=password]").fill(password);
  await page.locator("#kc-login").click();
  await expect(page.getByRole("navigation", { name: "Menú principal" })).toBeVisible();
}

test("la sesión sigue activa al revalidarse y una baja en Keycloak la corta", async ({
  page,
  request,
  context,
}) => {
  const username = "claims.dev";
  await login(page, username);
  try {
    // Pasado el intervalo, la revalidación real con Keycloak no expulsa a un usuario vigente.
    await expect
      .poll(
        async () => {
          await page.waitForTimeout(3500);
          return (await context.request.get("/bff/me")).status();
        },
        { timeout: 10_000 },
      )
      .toBe(200);

    await setEnabled(request, username, false);
    await expect
      .poll(async () => (await context.request.get("/bff/me")).status(), {
        timeout: 15_000,
        intervals: [1000],
      })
      .toBe(401);
  } finally {
    await setEnabled(request, username, true);
  }
});
