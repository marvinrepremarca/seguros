import { createHmac } from "node:crypto";

import { expect, test, type Page } from "@playwright/test";

// Solo con la API exigiendo MFA: make e2e-mfa. Prueba contra Keycloak real.
test.skip(!process.env["E2E_MFA"], "requiere make e2e-mfa");

function base32(secret: string): Buffer {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = "";
  for (const c of secret.replace(/[\s=]/g, "").toUpperCase()) {
    bits += alphabet.indexOf(c).toString(2).padStart(5, "0");
  }
  const bytes = bits.match(/.{8}/g) ?? [];
  return Buffer.from(bytes.map((b) => parseInt(b, 2)));
}

function totp(secret: string, at = Date.now()): string {
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 30_000)));
  const mac = createHmac("sha1", base32(secret)).update(counter).digest();
  const offset = (mac[mac.length - 1] ?? 0) & 0xf;
  return String((mac.readUInt32BE(offset) & 0x7fffffff) % 1_000_000).padStart(6, "0");
}

async function password(page: Page) {
  await page.goto("/login");
  await page.getByRole("link", { name: /ingresar|iniciar/i }).click();
  await page.locator("#username").fill("mfa.dev");
  await page.locator("#password").fill(process.env["E2E_PASSWORD"] ?? "");
  await page.locator("#kc-login").click();
}

test("un rol con MFA configura el segundo factor al entrar y luego se le exige", async ({
  browser,
}) => {
  test.setTimeout(120_000);
  const first = await browser.newContext();
  const page = await first.newPage();
  await password(page);

  // Primer ingreso: Keycloak obliga a configurar el segundo factor.
  await page.locator("#mode-manual").click();
  const secret = (await page.locator("#kc-totp-secret-key").innerText()).trim();
  await page.locator("#totp").fill(totp(secret));
  await page.locator("#saveTOTPBtn").click();

  // Configurarlo no cuenta como haberlo usado (amr solo trae pwd): la API lo rechaza y pide entrar de nuevo.
  await expect(page.getByRole("alert")).toContainText("dos pasos");

  // Un código no se puede repetir: espera al siguiente periodo.
  await new Promise((resolve) => setTimeout(resolve, 31_000));

  // El enlace pide autenticarse otra vez aunque haya sesión en Keycloak: ahora sí usa el código.
  await page.getByRole("link", { name: "Iniciar sesión" }).click();
  await page.locator("#password").fill(process.env["E2E_PASSWORD"] ?? "");
  await page.locator("#kc-login").click();
  await page.locator("#otp").fill(totp(secret));
  await page.locator("#kc-login").click();
  await expect(page.getByRole("button", { name: /cerrar sesión/i })).toBeVisible();
  await first.close();
});
