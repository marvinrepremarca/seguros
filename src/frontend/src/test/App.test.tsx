import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { signedIn, signedOut } from "./msw";

import { App } from "@/app/App";
import { createQueryClient } from "@/app/query-client";

function renderAt(path: string) {
  window.history.replaceState(null, "", path);
  return render(<App queryClient={createQueryClient()} />);
}

describe("App", () => {
  it("sin sesión redirige a /login conservando el destino", async () => {
    signedOut();
    renderAt("/modulos/billing");
    const link = await screen.findByRole("link", { name: /iniciar sesión/i });
    expect(link).toHaveAttribute("href", "/bff/auth/login?returnTo=%2Fmodulos%2Fbilling");
    expect(screen.getByRole("heading", { level: 1, name: "Seguros ERP" })).toBeInTheDocument();
  });

  it("muestra solo los módulos encendidos y lo permitido al rol", async () => {
    signedIn({
      flags: { "module.billing": true, "module.claims": false, "module.renewals": true },
    });
    renderAt("/");
    const nav = await screen.findByRole("navigation", { name: "Menú principal" });
    expect(within(nav).getByRole("link", { name: "Cartera y cobro" })).toBeInTheDocument();
    expect(within(nav).getByRole("link", { name: "Renovaciones" })).toBeInTheDocument();
    expect(within(nav).queryByRole("link", { name: "Siniestros" })).not.toBeInTheDocument();
    expect(within(nav).queryByRole("link", { name: "Comisiones" })).not.toBeInTheDocument();
    expect(within(nav).getByRole("link", { name: "Funcionalidades" })).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { level: 1, name: /Bienvenido, Ana Pérez/ }),
    ).toBeInTheDocument();
    expect(screen.getByText("Plan Básico")).toBeInTheDocument();
  });

  it("oculta la configuración a quien no tiene el permiso", async () => {
    signedIn({ roles: ["SalesAgent"], permissions: [], flags: { "module.crm": true } });
    renderAt("/");
    const nav = await screen.findByRole("navigation", { name: "Menú principal" });
    expect(within(nav).getByRole("link", { name: "Prospectos (CRM)" })).toBeInTheDocument();
    expect(within(nav).queryByRole("link", { name: "Funcionalidades" })).not.toBeInTheDocument();
  });

  it("la ruta de un módulo apagado responde como inexistente", async () => {
    signedIn({ flags: { "module.billing": true, "module.claims": false } });
    renderAt("/modulos/claims");
    expect(
      await screen.findByRole("heading", { name: "Página no encontrada" }),
    ).toBeInTheDocument();
  });

  it("la ruta de un módulo encendido se muestra", async () => {
    signedIn({ flags: { "module.billing": true } });
    renderAt("/modulos/billing");
    expect(
      await screen.findByRole("heading", { level: 1, name: "Cartera y cobro" }),
    ).toBeInTheDocument();
  });

  it("un módulo desconocido responde como inexistente", async () => {
    signedIn({ flags: { "module.billing": true } });
    renderAt("/modulos/inventado");
    expect(
      await screen.findByRole("heading", { name: "Página no encontrada" }),
    ).toBeInTheDocument();
  });

  it("muestra el motivo de un login rechazado", async () => {
    signedOut();
    renderAt("/login?error=identity.login.mfa_required");
    expect(await screen.findByRole("alert")).toHaveTextContent(/verificación en dos pasos/);
  });
});
