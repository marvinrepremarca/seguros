import { describe, expect, it } from "vitest";

import { amount, policyFormSchema, signedAmount } from "./schemas";

const base = {
  insurerId: "i",
  lineOfBusinessId: "l",
  productId: "",
  agentId: "",
  currency: "COP",
  netPremium: "1000000",
  taxes: "190000.50",
  effectiveFrom: "2026-01-01",
  effectiveTo: "2026-12-31",
  paymentForm: "single_payment",
  holderId: "h",
  parties: [{ partyId: "h", role: "insured" }],
  coverages: [],
  items: [],
} as const;

describe("policyFormSchema", () => {
  it("acepta una póliza válida", () => {
    expect(policyFormSchema.safeParse(base).success).toBe(true);
  });

  it("la fecha final debe ser posterior", () => {
    expect(policyFormSchema.safeParse({ ...base, effectiveTo: "2026-01-01" }).success).toBe(false);
  });

  it("exige un asegurado", () => {
    expect(policyFormSchema.safeParse({ ...base, parties: [] }).success).toBe(false);
    expect(
      policyFormSchema.safeParse({ ...base, parties: [{ partyId: "b", role: "beneficiary" }] })
        .success,
    ).toBe(false);
  });
});

describe("montos", () => {
  it("solo decimales con punto y hasta 4 decimales", () => {
    expect(amount.safeParse("1000000.1234").success).toBe(true);
    for (const bad of ["1.000.000", "1,5", "-1", "1.12345", ""]) {
      expect(amount.safeParse(bad).success).toBe(false);
    }
  });

  it("las variaciones de un endoso pueden ser negativas", () => {
    expect(signedAmount.safeParse("-100000").success).toBe(true);
    expect(signedAmount.safeParse("abc").success).toBe(false);
  });
});
