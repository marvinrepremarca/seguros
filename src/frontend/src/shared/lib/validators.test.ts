import { describe, expect, it } from "vitest";

import { ccSchema, isValidCC, isValidNIT, nitCheckDigit, nitSchema } from "./validators";

describe("cédula", () => {
  it.each(["1032456789", "1.032.456.789", "123", "79123456"])("acepta %s", (v) => {
    expect(isValidCC(v)).toBe(true);
  });
  it.each(["", "12", "01234567", "12345678901", "12a456", "-123"])("rechaza %s", (v) => {
    expect(isValidCC(v)).toBe(false);
  });
  it("normaliza con zod", () => {
    expect(ccSchema.parse("1.032.456.789")).toBe("1032456789");
    expect(ccSchema.safeParse("12").success).toBe(false);
  });
});

describe("NIT", () => {
  it("calcula el dígito de verificación (algoritmo DIAN)", () => {
    expect(nitCheckDigit("900123456")).toBe(8);
    expect(nitCheckDigit("860002964")).toBe(4);
    expect(nitCheckDigit("800197268")).toBe(4);
  });
  it.each(["900123456-8", "9001234568", "900.123.456-8", "860002964-4"])("acepta %s", (v) => {
    expect(isValidNIT(v)).toBe(true);
  });
  it.each(["900123456-7", "900123456", "12345-1", "abc"])("rechaza %s", (v) => {
    expect(isValidNIT(v)).toBe(false);
  });
  it("valida con zod", () => {
    expect(nitSchema.parse("900.123.456-8")).toBe("900123456-8");
    expect(nitSchema.safeParse("900123456-7").error?.issues[0]?.message).toMatch(/dígito/);
  });
});
