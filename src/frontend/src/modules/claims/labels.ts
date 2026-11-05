import type { ClaimStatus } from "./api/generated/claims";

export const CLAIM_LABELS: Readonly<Record<ClaimStatus, string>> = {
  reported: "Reportado",
  notified: "Avisado a la aseguradora",
  documenting: "En documentación",
  evaluating: "En evaluación",
  approved: "Aprobado",
  objected: "Objetado",
  paid: "Pagado",
  closed: "Cerrado",
};
