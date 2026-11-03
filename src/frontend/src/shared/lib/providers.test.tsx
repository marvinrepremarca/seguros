import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { features, isFeatureKey } from "./features";
import { FeatureGate, FeaturesProvider } from "./providers";
import { safeExternalUrl, safeReturnTo } from "./session";

describe("FeatureGate", () => {
  it("muestra el contenido con el flag encendido", () => {
    render(
      <FeaturesProvider value={{ "module.claims": true }}>
        <FeatureGate flag={features.claims.module}>Siniestros</FeatureGate>
      </FeaturesProvider>,
    );
    expect(screen.getByText("Siniestros")).toBeInTheDocument();
  });

  it("oculta el contenido con el flag apagado o ausente (fail-closed)", () => {
    render(
      <FeaturesProvider value={{ "module.claims": false }}>
        <FeatureGate flag={features.claims.module} fallback={<span>No disponible</span>}>
          Siniestros
        </FeatureGate>
        <FeatureGate flag={features.billing.module}>Cartera</FeatureGate>
      </FeaturesProvider>,
    );
    expect(screen.queryByText("Siniestros")).not.toBeInTheDocument();
    expect(screen.queryByText("Cartera")).not.toBeInTheDocument();
    expect(screen.getByText("No disponible")).toBeInTheDocument();
  });

  it("reconoce solo claves registradas", () => {
    expect(isFeatureKey("module.claims")).toBe(true);
    expect(isFeatureKey("module.inventado")).toBe(false);
  });
});

describe("redirecciones seguras", () => {
  it.each([
    ["/polizas?x=1", "/polizas?x=1"],
    ["//evil.example", "/"],
    ["https://evil.example", "/"],
    ["/\\evil", "/"],
    [undefined, "/"],
  ])("safeReturnTo(%s) = %s", (input, want) => {
    expect(safeReturnTo(input)).toBe(want);
  });

  it("safeExternalUrl solo acepta http(s)", () => {
    expect(safeExternalUrl("https://id.example/logout?x=1")).toBe("https://id.example/logout?x=1");
    expect(safeExternalUrl(["javascript", "alert(1)"].join(":"))).toBeNull();
    expect(safeExternalUrl("no es url")).toBeNull();
  });
});
