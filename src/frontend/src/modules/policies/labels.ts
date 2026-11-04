// Textos de la UI de pólizas (es-CO).
import type {
  CancellationCause,
  EndorsementType,
  PartyRole,
  PaymentForm,
  PolicyStatus,
} from "./api/generated/policies";

export const STATUS_LABELS: Readonly<Record<PolicyStatus, string>> = {
  in_quotation: "En cotización",
  in_process: "En trámite",
  active: "Vigente",
  in_arrears: "En mora",
  cancelled: "Cancelada",
  expired: "Vencida",
  renewed: "Renovada",
  not_renewed: "No renovada",
  rejected: "Rechazada",
};

export const PAYMENT_LABELS: Readonly<Record<PaymentForm, string>> = {
  single_payment: "Contado",
  monthly_installments: "Cuotas mensuales",
  quarterly_installments: "Cuotas trimestrales",
  automatic_debit: "Débito automático",
  premium_financing: "Financiación de primas",
};

export const ROLE_LABELS: Readonly<Record<PartyRole, string>> = {
  holder: "Tomador",
  insured: "Asegurado",
  beneficiary: "Beneficiario",
};

export const CAUSE_LABELS: Readonly<Record<CancellationCause, string>> = {
  client_request: "Solicitud del cliente",
  item_sold: "Venta del bien",
  arrears: "Mora en el pago",
  insurer_revocation: "Revocación de la aseguradora",
  other: "Otra",
};

export const ENDORSEMENT_LABELS: Readonly<Record<EndorsementType, string>> = {
  data_change: "Cambio de datos",
  item_change: "Cambio del bien asegurado",
  party_inclusion: "Inclusión de asegurado o beneficiario",
  party_exclusion: "Exclusión de asegurado o beneficiario",
  sum_insured_increase: "Aumento de suma asegurada",
  sum_insured_decrease: "Disminución de suma asegurada",
  other: "Otro",
};

export const ENDORSEMENT_STATUS_LABELS = {
  requested: "Solicitado",
  issued: "Emitido",
  rejected: "Rechazado",
} as const;
