// Validadores de documentos colombianos (rules/frontend.md §Formularios). Son validaciones de
// formato para la UX; el backend valida siempre.
import { z } from "zod";

/** Pesos oficiales de la DIAN para el dígito de verificación del NIT (de derecha a izquierda). */
const NIT_WEIGHTS = [3, 7, 13, 17, 19, 23, 29, 37, 41, 43, 47, 53, 59, 67, 71] as const;

/** Quita puntos, espacios y guiones de separación de miles. */
export function normalizeDocument(value: string): string {
  return value.replace(/[.\s]/g, "").trim();
}

/** Cédula de ciudadanía: solo dígitos, 3 a 10, sin cero inicial. */
export function isValidCC(value: string): boolean {
  return /^[1-9]\d{2,9}$/.test(normalizeDocument(value));
}

/** Calcula el dígito de verificación de un NIT (base sin DV). */
export function nitCheckDigit(base: string): number {
  if (!/^\d{1,15}$/.test(base)) throw new RangeError("NIT inválido");
  let sum = 0;
  for (let i = 0; i < base.length; i++) {
    sum += Number(base.charAt(base.length - 1 - i)) * (NIT_WEIGHTS[i] ?? 0);
  }
  const r = sum % 11;
  return r > 1 ? 11 - r : r;
}

/** NIT con dígito de verificación ("900123456-8" o "9001234568"). */
export function isValidNIT(value: string): boolean {
  const v = normalizeDocument(value);
  const m = /^(\d{6,14})-?(\d)$/.exec(v);
  if (!m) return false;
  const [, base, dv] = m;
  return base !== undefined && dv !== undefined && nitCheckDigit(base) === Number(dv);
}

export const ccSchema = z
  .string()
  .transform(normalizeDocument)
  .refine(isValidCC, { message: "La cédula debe tener entre 3 y 10 dígitos" });

export const nitSchema = z
  .string()
  .transform(normalizeDocument)
  .refine(isValidNIT, { message: "El NIT o su dígito de verificación no es válido" });
