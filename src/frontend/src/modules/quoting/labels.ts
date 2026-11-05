import type { RequestStatus } from "./api/generated/quoting";

export const REQUEST_LABELS: Readonly<Record<RequestStatus, string>> = {
  open: "Abierta",
  accepted: "Aceptada",
  declined: "Cerrada sin aceptar",
};
