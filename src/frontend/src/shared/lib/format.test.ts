import { describe, expect, it } from "vitest";

import { formatDate, formatDateTime, formatMoney, mask } from "./format";

// Intl usa espacio duro entre el símbolo y el número.
const NBSP = String.fromCharCode(160);
const nbsp = (s: string) => s.replaceAll(NBSP, " ");

describe("formatMoney", () => {
  it("formatea COP sin decimales con separador de miles", () => {
    expect(nbsp(formatMoney("1234567"))).toBe("$ 1.234.567");
    expect(nbsp(formatMoney("1234567.4999"))).toBe("$ 1.234.567");
  });
  it("redondea half-even sin pasar por number", () => {
    expect(nbsp(formatMoney("0.5"))).toBe("$ 0");
    expect(nbsp(formatMoney("1.5"))).toBe("$ 2");
    expect(nbsp(formatMoney("99999999999999999999.5"))).toBe("$ 100.000.000.000.000.000.000");
  });
  it("otra moneda con dos decimales", () => {
    expect(formatMoney("10.005", "USD")).toContain("10,00");
  });
  it("rechaza valores no decimales", () => {
    expect(() => formatMoney("1e3")).toThrow();
    expect(() => formatMoney("abc")).toThrow();
  });
});

describe("formatDate", () => {
  it("dd/MM/yyyy sin corrimiento de zona", () => {
    expect(formatDate("2026-01-01")).toBe("01/01/2026");
    expect(formatDate("2026-12-31")).toBe("31/12/2026");
  });
  it("rechaza formatos inválidos", () => {
    expect(() => formatDate("01/01/2026")).toThrow();
  });
});

describe("formatDateTime", () => {
  it("usa la hora de Bogotá", () => {
    expect(formatDateTime("2026-11-03T05:30:00Z")).toBe("03/11/2026, 00:30");
  });
  it("rechaza instantes inválidos", () => {
    expect(() => formatDateTime("ayer")).toThrow();
  });
});

describe("mask", () => {
  it("muestra solo los últimos dígitos", () => {
    expect(mask("1032456789")).toBe("•••• 6789");
    expect(mask("123")).toBe("•••");
  });
});
