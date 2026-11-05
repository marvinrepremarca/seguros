import type {
  AgingBucket,
  CollectionChannel,
  InstallmentStatus,
  PaymentMethod,
  ReportedBy,
} from "./api/generated/billing";

export const BUCKET_LABELS: Readonly<Record<AgingBucket, string>> = {
  not_due: "Por vencer",
  d0_30: "0 a 30 días",
  d31_60: "31 a 60 días",
  d61_90: "61 a 90 días",
  d90_plus: "Más de 90 días",
};

export const STATUS_LABELS: Readonly<Record<InstallmentStatus, string>> = {
  pending: "Pendiente",
  paid: "Pagada",
  voided: "Anulada",
};

export const METHOD_LABELS: Readonly<Record<PaymentMethod, string>> = {
  transfer: "Transferencia",
  card: "Tarjeta",
  cash: "Efectivo",
  check: "Cheque",
  automatic_debit: "Débito automático",
  financing: "Financiación",
  other: "Otro",
};

export const REPORTED_LABELS: Readonly<Record<ReportedBy, string>> = {
  client: "Cliente",
  insurer: "Aseguradora",
};

export const CHANNEL_LABELS: Readonly<Record<CollectionChannel, string>> = {
  call: "Llamada",
  message: "Mensaje",
  email: "Correo",
  visit: "Visita",
  other: "Otro",
};
