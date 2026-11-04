import { describe, expect, it } from "vitest";

import { isValidDocument, partyFormSchema } from "./schemas";

const base = {
  kind: "person",
  name: "Ana Pérez",
  documentType: "CC",
  documentNumber: "1020304050",
  email: "",
  phone: "",
  address: "",
  city: "",
} as const;

describe("partyFormSchema", () => {
  it("acepta una persona con CC", () => {
    expect(partyFormSchema.safeParse(base).success).toBe(true);
  });

  it("una organización exige NIT con dígito de verificación válido", () => {
    const org = { ...base, kind: "organization", documentType: "NIT" } as const;
    expect(partyFormSchema.safeParse({ ...org, documentNumber: "900123456-8" }).success).toBe(true);
    expect(partyFormSchema.safeParse({ ...org, documentNumber: "900123456-1" }).success).toBe(
      false,
    );
    expect(partyFormSchema.safeParse({ ...base, kind: "organization" }).success).toBe(false);
  });

  it("rechaza correo inválido y acepta vacío", () => {
    expect(partyFormSchema.safeParse({ ...base, email: "ana" }).success).toBe(false);
    expect(partyFormSchema.safeParse({ ...base, email: "ana@example.com" }).success).toBe(true);
  });
});

describe("isValidDocument", () => {
  it("CE y PPT son alfanuméricos", () => {
    expect(isValidDocument("CE", "AB12345")).toBe(true);
    expect(isValidDocument("PPT", "12#45")).toBe(false);
  });
});
