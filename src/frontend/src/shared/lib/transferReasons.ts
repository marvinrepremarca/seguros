/** Catálogo de motivos de traspaso (DECISIONS N12). */
export const TRANSFER_REASONS = [
  { value: "employee_departure", label: "Retiro del asesor" },
  { value: "role_change", label: "Cambio de cargo del asesor" },
  { value: "prolonged_absence", label: "Ausencia prolongada del asesor" },
  { value: "portfolio_redistribution", label: "Redistribución de cartera" },
  { value: "client_request", label: "Solicitud del cliente" },
  { value: "branch_change", label: "Cambio de sucursal o zona" },
  { value: "assignment_correction", label: "Corrección de asignación" },
  { value: "other", label: "Otro" },
] as const;

export type TransferReasonCode = (typeof TRANSFER_REASONS)[number]["value"];

/** El formulario exige el motivo: un valor vacío aquí es un error de programación. */
export function requiredReason(reason: TransferReasonCode | ""): TransferReasonCode {
  if (reason === "") throw new Error("motivo requerido");
  return reason;
}
