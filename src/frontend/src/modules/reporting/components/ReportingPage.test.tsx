import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type * as generated from "../api/generated/reporting";
import { getKpis } from "../api/generated/reporting";
import { ReportingPage } from "./ReportingPage";

vi.mock("../api/generated/reporting", async (original) => ({
  ...(await original<typeof generated>()),
  getKpis: vi.fn(),
}));

function show() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ReportingPage today="2026-06-15" />
    </QueryClientProvider>,
  );
}

describe("ReportingPage", () => {
  it("omite las secciones de los módulos apagados", async () => {
    vi.mocked(getKpis).mockResolvedValue({
      status: 200,
      headers: new Headers(),
      data: {
        premiums: [{ currency: "COP", amount: "1000" }],
        issuanceDays: "1",
        policiesPerClient: { policies: 2, clients: 1 },
      },
    });
    show();
    expect(await screen.findByRole("heading", { name: "Primas emitidas" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Retención" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Siniestralidad" })).not.toBeInTheDocument();
  });

  it("muestra razones y evita dividir por cero", async () => {
    vi.mocked(getKpis).mockResolvedValue({
      status: 200,
      headers: new Headers(),
      data: {
        premiums: [],
        issuanceDays: "0",
        policiesPerClient: { policies: 0, clients: 0 },
        retention: { due: 4, renewed: 3 },
        conversion: { requests: 0, accepted: 0 },
      },
    });
    show();
    expect(await screen.findByText("75.0 % (3 de 4)")).toBeInTheDocument();
    expect(screen.getByText("Sin datos")).toBeInTheDocument();
  });
});
