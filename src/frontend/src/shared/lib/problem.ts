// Traduce errores problem+json a mensajes en español. Los códigos son el contrato estable del backend.
import { ApiError } from "./api-client";

const MESSAGES: Readonly<Record<string, string>> = {
  "authz.forbidden": "No tienes permiso para esta operación.",
  "platform.bad_request": "Revisa los datos: hay campos con un formato inválido.",
  "platform.rate_limited": "Demasiadas solicitudes. Espera un momento e inténtalo de nuevo.",
  "kernel.date_range.invalid": "La fecha final debe ser posterior a la inicial.",
  "parties.party.document_taken": "Ya existe un cliente con ese documento.",
  "parties.party.invalid_document": "El número de documento no es válido para su tipo.",
  "parties.party.document_kind_mismatch": "Una organización usa NIT; una persona, CC, CE o PPT.",
  "parties.party.invalid_contact": "Revisa el correo y los datos de contacto.",
  "parties.party.concurrent_update":
    "Otra persona cambió este cliente. Recarga e inténtalo de nuevo.",
  "parties.party.not_found": "No se encontró el cliente.",
  "parties.consent.future_date": "La fecha de la autorización no puede ser futura.",
  "catalog.item.name_taken": "Ya existe un registro con ese nombre.",
  "catalog.item.concurrent_update":
    "Otra persona cambió este registro. Recarga e inténtalo de nuevo.",
  "catalog.reference.invalid":
    "La aseguradora, el ramo o el producto no existe, está inactivo o no corresponde.",
  "catalog.risk_data.invalid":
    "Revisa los datos del bien asegurado: no cumplen lo que exige el ramo.",
  "catalog.commission_agreement.overlap": "Ya hay un acuerdo para ese ámbito en esas fechas.",
  "kernel.percentage.invalid": "El porcentaje debe estar entre 0 y 100.",
  "policies.policy.concurrent_update":
    "Otra persona cambió esta póliza. Recarga e inténtalo de nuevo.",
  "policies.policy.invalid_transition": "La póliza no permite esta operación en su estado actual.",
  "policies.policy.not_editable": "Después de expedida, la póliza solo cambia por endoso.",
  "policies.policy.number_taken": "Ese número de póliza ya existe en la aseguradora.",
  "policies.policy.invalid_holder": "La póliza debe tener exactamente un tomador.",
  "policies.policy.insured_required": "La póliza debe tener al menos un asegurado.",
  "policies.policy.invalid_party": "Hay una parte repetida o que no existe.",
  "policies.issue.kyc_not_approved":
    "No se puede expedir: el conocimiento del cliente (SARLAFT) del tomador no está aprobado.",
  "policies.issue.compliance_unavailable":
    "No se pudo verificar el requisito de SARLAFT. La póliza no se expidió; inténtalo de nuevo en unos minutos.",
  "policies.cancellation.outside_coverage_period":
    "La fecha de cancelación está fuera de la vigencia.",
  "policies.cancellation.invalid_unearned_premium":
    "La prima no devengada debe estar entre 0 y la prima neta.",
  "policies.endorsement.outside_coverage_period": "La fecha del endoso está fuera de la vigencia.",
  "policies.endorsement.policy_not_active": "Los endosos aplican a pólizas vigentes o en mora.",
  "policies.endorsement.negative_premium": "La prima o los impuestos quedarían negativos.",
  "policies.endorsement.holder_change_not_allowed":
    "Un endoso no puede cambiar el tomador de la póliza.",
  "policies.endorsement.not_pending": "Este endoso ya fue resuelto.",
};

function problemCode(problem: unknown): string | undefined {
  if (typeof problem !== "object" || problem === null || !("type" in problem)) return undefined;
  return typeof problem.type === "string" ? problem.type : undefined;
}

/** Mensaje para mostrar al usuario; nunca expone detalles internos. */
export function problemMessage(error: unknown): string | null {
  if (error === null || error === undefined) return null;
  if (error instanceof ApiError) {
    const code = problemCode(error.problem);
    if (code !== undefined && MESSAGES[code] !== undefined) return MESSAGES[code];
    if (error.status === 403) return "No tienes permiso para esta operación.";
    if (error.status === 404) return "No se encontró el registro.";
  }
  return "No se pudo completar la operación. Inténtalo de nuevo.";
}
