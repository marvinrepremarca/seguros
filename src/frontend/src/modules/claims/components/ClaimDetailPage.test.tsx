import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { SessionContext, permissions } from "@/shared/lib/session";

import type * as generated from "../api/generated/claims";
import { getClaim } from "../api/generated/claims";
import { ClaimDetailPage } from "./ClaimDetailPage";

vi.mock("../api/generated/claims", async (original) => ({
  ...(await original<typeof generated>()),
  getClaim: vi.fn(),
  getClaimTracking: vi.fn().mockResolvedValue({ status: 200, data: { checklist: [], notes: [] } }),
}));

function show(perms: string[]) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <SessionContext
        value={{
          user: { id: "u", displayName: "U" },
          tenant: { id: "t", plan: "profesional" },
          roles: [],
          permissions: perms,
        }}
      >
        <ClaimDetailPage claimId="c" />
      </SessionContext>
    </QueryClientProvider>,
  );
}

describe("ClaimDetailPage", () => {
  beforeEach(() => {
    vi.mocked(getClaim).mockResolvedValue({
      status: 200,
      data: {
        id: "c",
        policyId: "p",
        insurerId: "i",
        holderPartyId: "h",
        holderName: "Ana",
        occurredOn: "2026-06-01",
        location: "Bogotá",
        claimType: "Choque",
        description: "Choque simple",
        currency: "COP",
        status: "evaluating",
        alertOutOfCoverage: true,
        alertInArrears: false,
        alertNotInForce: false,
        reportedAt: "2026-06-02T10:00:00Z",
        version: 1,
      },
      headers: new Headers(),
    });
  });

  it("al asesor solo le muestra el encabezado, con alertas", async () => {
    show([permissions.claimReadOwn]);
    expect(await screen.findByText(/fuera de la vigencia/)).toBeTruthy();
    expect(screen.queryByText("Gestión")).toBeNull();
    expect(screen.queryByText("Detalle de salud")).toBeNull();
    expect(screen.queryByText("Soportes")).toBeNull();
  });

  it("al analista le ofrece solo las transiciones válidas", async () => {
    show([permissions.claimRead, permissions.claimManage, permissions.claimHealthRead]);
    expect(await screen.findByText("Pasar a: Aprobado")).toBeTruthy();
    expect(screen.getByText("Pasar a: Objetado")).toBeTruthy();
    expect(screen.queryByText("Pasar a: Cerrado")).toBeNull();
    expect(screen.getByText("Detalle de salud")).toBeTruthy();
    expect(screen.queryByText("Ver detalle")).toBeNull();
  });
});
