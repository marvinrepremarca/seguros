import { describe, expect, it } from "vitest";

import { loginErrorMessage } from "./index";

describe("loginErrorMessage", () => {
  it.each([
    ["identity.login.mfa_required", /dos pasos/],
    ["identity.login.tenant_unavailable", /no tiene acceso activo/],
    ["identity.login.organization_required", /agencia/],
    ["identity.login.no_roles", /roles/],
    ["identity.login.failed", /Inténtalo de nuevo/],
    ["codigo.desconocido", /Inténtalo de nuevo/],
  ])("%s", (code, re) => {
    expect(loginErrorMessage(code)).toMatch(re);
  });

  it("sin código no hay mensaje", () => {
    expect(loginErrorMessage(undefined)).toBeNull();
    expect(loginErrorMessage("")).toBeNull();
  });
});
