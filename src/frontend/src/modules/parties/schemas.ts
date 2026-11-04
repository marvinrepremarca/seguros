// Validación de formularios de terceros: los límites vienen del contrato (zod generado) y aquí se
// añaden los documentos colombianos. El backend valida siempre.
import { z } from "zod";

import {
  CreatePartyBody,
  createPartyBodyAddressMax,
  createPartyBodyCityMax,
  createPartyBodyNameMax,
  createPartyBodyNameMin,
  createPartyBodyPhoneMax,
  registerPartyConsentBodyPurposeMax,
  registerPartyConsentBodyPurposeMin,
  registerPartyConsentBodyTextVersionMax,
} from "./api/generated/parties.zod";

import { isValidCC, isValidNIT, normalizeDocument } from "@/shared/lib/validators";

export const DOCUMENT_LABELS = {
  CC: "Cédula de ciudadanía",
  CE: "Cédula de extranjería",
  PPT: "Permiso por protección temporal",
  NIT: "NIT",
} as const;

export type DocumentTypeCode = keyof typeof DOCUMENT_LABELS;

/** Valida el número según el tipo. CE y PPT son alfanuméricos. */
export function isValidDocument(type: DocumentTypeCode, value: string): boolean {
  switch (type) {
    case "CC":
      return isValidCC(value);
    case "NIT":
      return isValidNIT(value);
    case "CE":
    case "PPT":
      return /^[A-Za-z0-9]{3,15}$/.test(normalizeDocument(value));
  }
}

const name = z
  .string()
  .trim()
  .min(createPartyBodyNameMin, `Mínimo ${createPartyBodyNameMin} caracteres`)
  .max(createPartyBodyNameMax, `Máximo ${createPartyBodyNameMax} caracteres`);

const contact = {
  email: z.union([z.literal(""), z.email("Correo inválido")]),
  phone: z.string().max(createPartyBodyPhoneMax, `Máximo ${createPartyBodyPhoneMax} caracteres`),
  address: z
    .string()
    .max(createPartyBodyAddressMax, `Máximo ${createPartyBodyAddressMax} caracteres`),
  city: z.string().max(createPartyBodyCityMax, `Máximo ${createPartyBodyCityMax} caracteres`),
};

export const partyFormSchema = z
  .object({
    kind: CreatePartyBody.shape.kind,
    name,
    documentType: z.enum(["CC", "CE", "PPT", "NIT"]),
    documentNumber: z.string().min(1, "Obligatorio"),
    ...contact,
  })
  .superRefine((v, ctx) => {
    if ((v.kind === "organization") !== (v.documentType === "NIT")) {
      ctx.addIssue({
        code: "custom",
        path: ["documentType"],
        message: "Una organización usa NIT; una persona, CC, CE o PPT",
      });
    }
    if (!isValidDocument(v.documentType, v.documentNumber)) {
      ctx.addIssue({
        code: "custom",
        path: ["documentNumber"],
        message: "El número no es válido para este tipo de documento",
      });
    }
  });

export type PartyFormValues = z.infer<typeof partyFormSchema>;

export const partyEditSchema = z.object({
  name,
  ...contact,
});

export type PartyEditValues = z.infer<typeof partyEditSchema>;

export const consentFormSchema = z.object({
  status: z.enum(["granted", "revoked"]),
  purpose: z
    .string()
    .trim()
    .min(
      registerPartyConsentBodyPurposeMin,
      `Mínimo ${registerPartyConsentBodyPurposeMin} caracteres`,
    )
    .max(
      registerPartyConsentBodyPurposeMax,
      `Máximo ${registerPartyConsentBodyPurposeMax} caracteres`,
    ),
  textVersion: z
    .string()
    .trim()
    .min(1, "Obligatorio")
    .max(
      registerPartyConsentBodyTextVersionMax,
      `Máximo ${registerPartyConsentBodyTextVersionMax} caracteres`,
    ),
  consentDate: z.iso.date("Fecha inválida"),
});

export type ConsentFormValues = z.infer<typeof consentFormSchema>;
