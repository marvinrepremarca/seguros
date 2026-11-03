// Formateadores es-CO (DECISIONS.md P4): moneda COP, fechas dd/MM/yyyy, zona America/Bogota.
// Los montos llegan como string decimal y nunca se convierten a number (rules/frontend.md).
import Decimal from "decimal.js";

export const BUSINESS_TIME_ZONE = "America/Bogota";

const DECIMAL_PATTERN = /^-?\d+(\.\d+)?$/;
const DATE_PATTERN = /^(\d{4})-(\d{2})-(\d{2})$/;

/** Formatea un monto decimal (string) como moneda. Lanza si el valor no es un decimal válido. */
export function formatMoney(amount: string, currency = "COP"): string {
  if (!DECIMAL_PATTERN.test(amount)) {
    throw new RangeError("monto inválido");
  }
  const digits = currency === "COP" ? 0 : 2;
  // Redondeo explícito de presentación (half-even) sobre el decimal, sin pasar por number.
  const rounded = new Decimal(amount)
    .toDecimalPlaces(digits, Decimal.ROUND_HALF_EVEN)
    .toFixed(digits);
  const formatter = new Intl.NumberFormat("es-CO", {
    style: "currency",
    currency,
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  });
  // Intl acepta strings numéricos (StringNumericLiteral) con precisión decimal completa.
  return formatter.format(rounded as Intl.StringNumericLiteral); // eslint-disable-line @typescript-eslint/consistent-type-assertions -- validado arriba con DECIMAL_PATTERN
}

/** Formatea una fecha de vigencia (YYYY-MM-DD, sin zona) como dd/MM/yyyy sin crear un Date. */
export function formatDate(isoDate: string): string {
  const m = DATE_PATTERN.exec(isoDate);
  if (!m) throw new RangeError("fecha inválida");
  const [, y, mo, d] = m;
  return `${d ?? ""}/${mo ?? ""}/${y ?? ""}`;
}

/** Formatea un instante (RFC 3339) en la zona de negocio. */
export function formatDateTime(instant: string): string {
  const date = new Date(instant);
  if (Number.isNaN(date.getTime())) throw new RangeError("instante inválido");
  return new Intl.DateTimeFormat("es-CO", {
    timeZone: BUSINESS_TIME_ZONE,
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).format(date);
}

/** Enmascara un dato Restringido mostrando solo los últimos `visible` caracteres. */
export function mask(value: string, visible = 4): string {
  if (value.length <= visible) return "•".repeat(value.length);
  return `•••• ${value.slice(-visible)}`;
}
