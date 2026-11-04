// Validación de formularios de pólizas. Los montos se manejan como texto decimal, nunca como número.
import { z } from "zod";

const AMOUNT = /^\d{1,15}(\.\d{1,4})?$/;
const SIGNED_AMOUNT = /^-?\d{1,15}(\.\d{1,4})?$/;

export const amount = z
  .string()
  .trim()
  .regex(AMOUNT, "Monto inválido: usa punto para los decimales");
export const signedAmount = z
  .string()
  .trim()
  .regex(SIGNED_AMOUNT, "Monto inválido: usa punto para los decimales");
const date = z.iso.date("Fecha inválida");

export const policyFormSchema = z
  .object({
    insurerId: z.string().min(1, "Obligatorio"),
    lineOfBusinessId: z.string().min(1, "Obligatorio"),
    productId: z.string(),
    agentId: z.string(),
    currency: z.enum(["COP", "USD", "EUR"]),
    netPremium: amount,
    taxes: amount,
    effectiveFrom: date,
    effectiveTo: date,
    paymentForm: z.enum([
      "single_payment",
      "monthly_installments",
      "quarterly_installments",
      "automatic_debit",
      "premium_financing",
    ]),
    holderId: z.string().min(1, "Selecciona el tomador"),
    parties: z.array(
      z.object({
        partyId: z.string().min(1, "Selecciona el cliente"),
        role: z.enum(["insured", "beneficiary"]),
      }),
    ),
    coverages: z.array(
      z.object({
        name: z.string().trim().min(2, "Mínimo 2 caracteres").max(200, "Máximo 200 caracteres"),
        sumInsured: amount,
        deductible: z.string().max(200, "Máximo 200 caracteres"),
      }),
    ),
    items: z.array(
      z.object({
        description: z
          .string()
          .trim()
          .min(2, "Mínimo 2 caracteres")
          .max(200, "Máximo 200 caracteres"),
        risk: z.record(z.string(), z.string()),
      }),
    ),
  })
  .refine((v) => v.effectiveTo > v.effectiveFrom, {
    path: ["effectiveTo"],
    message: "Debe ser posterior a la fecha inicial",
  })
  .refine((v) => v.parties.some((p) => p.role === "insured"), {
    path: ["parties"],
    message: "Agrega al menos un asegurado",
  });

export type PolicyFormValues = z.infer<typeof policyFormSchema>;
