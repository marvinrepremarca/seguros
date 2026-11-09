import { describe, expect, it } from "vitest";

import { NAV_SECTIONS, visibleSections } from "./navigation";

describe("visibleSections", () => {
  it("Pólizas se ve con cualquiera de sus dos permisos de lectura", () => {
    const labels = (perm: string) =>
      visibleSections(
        NAV_SECTIONS,
        () => false,
        (p) => p === perm,
      ).flatMap((x) => x.items.map((i) => i.label));
    expect(labels("policies.policy.read")).toContain("Pólizas");
    expect(labels("policies.policy.read-own")).toContain("Pólizas");
    expect(labels("parties.party.read")).not.toContain("Pólizas");
  });

  it("sin flags ni permisos solo queda Inicio", () => {
    const s = visibleSections(
      NAV_SECTIONS,
      () => false,
      () => false,
    );
    expect(s.map((x) => x.title)).toEqual(["General"]);
    expect(s[0]?.items.map((i) => i.label)).toEqual(["Inicio"]);
  });

  it("todo encendido muestra todos los módulos", () => {
    const s = visibleSections(
      NAV_SECTIONS,
      () => true,
      () => true,
    );
    const labels = s.flatMap((x) => x.items.map((i) => i.label));
    expect(labels).toContain("Comisiones");
    expect(labels).toContain("Funcionalidades");
    expect(labels).toContain("Pólizas");
    expect(labels).toContain("Catálogo");
    expect(labels).toContain("Traspaso de cartera");
    expect(labels).toHaveLength(18); // Inicio, Clientes, Pólizas + 11 módulos + Funcionalidades, Catálogo, Parámetros, Traspaso
  });
});
