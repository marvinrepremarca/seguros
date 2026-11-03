import { describe, expect, it } from "vitest";

import { MODULE_LABELS, NAV_SECTIONS, visibleSections } from "./navigation";

describe("visibleSections", () => {
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
    expect(labels).toHaveLength(13); // Inicio + 11 módulos + Funcionalidades
  });

  it("cada módulo del menú tiene etiqueta", () => {
    expect(MODULE_LABELS["billing"]).toBe("Cartera y cobro");
    expect(MODULE_LABELS["integrations"]).toBe("Integraciones");
  });
});
