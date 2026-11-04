import { describe, expect, it } from "vitest";

import { fromRiskData, riskFields, toRiskData } from "./risk";

const schema = {
  type: "object",
  required: ["plate", "year"],
  properties: {
    year: { type: "integer", "x-label": "Año", "x-order": 2 },
    plate: { type: "string", "x-label": "Placa", "x-order": 1, pattern: "^[A-Z]{3}[0-9]{3}$" },
    use: { type: "string", "x-label": "Uso", "x-order": 3 },
  },
};

describe("riskFields", () => {
  it("ordena por x-order y marca los obligatorios", () => {
    const f = riskFields(schema);
    expect(f.map((x) => x.name)).toEqual(["plate", "year", "use"]);
    expect(f[0]).toMatchObject({ label: "Placa", required: true, pattern: "^[A-Z]{3}[0-9]{3}$" });
    expect(f[2]?.required).toBe(false);
  });

  it("sin esquema no hay campos", () => {
    expect(riskFields(undefined)).toEqual([]);
    expect(riskFields({})).toEqual([]);
  });
});

describe("toRiskData", () => {
  const fields = riskFields(schema);

  it("convierte enteros y omite vacíos", () => {
    expect(toRiskData(fields, { plate: " ABC123 ", year: "2024", use: "" })).toEqual({
      plate: "ABC123",
      year: 2024,
    });
  });

  it("un entero mal escrito se envía tal cual para que el backend lo rechace", () => {
    expect(toRiskData(fields, { year: "dos mil" })).toEqual({ year: "dos mil" });
  });

  it("ida y vuelta con datos guardados", () => {
    const stored = { plate: "ABC123", year: 2024 };
    expect(toRiskData(fields, fromRiskData(stored))).toEqual(stored);
  });
});
