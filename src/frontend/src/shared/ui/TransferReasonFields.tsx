import { TRANSFER_REASONS, type TransferReasonCode } from "@/shared/lib/transferReasons";

import { Field, Select, Textarea } from "./field";

/** Motivo y nota de un cambio de asesor. La nota no debe llevar datos sensibles. */
export function TransferReasonFields({
  reason,
  note,
  onReasonChange,
  onNoteChange,
}: {
  reason: TransferReasonCode | "";
  note: string;
  onReasonChange: (value: TransferReasonCode | "") => void;
  onNoteChange: (value: string) => void;
}) {
  return (
    <>
      <Field label="Motivo del cambio">
        {(p) => (
          <Select
            {...p}
            required
            value={reason}
            onChange={(e) => {
              onReasonChange(TRANSFER_REASONS.find((r) => r.value === e.target.value)?.value ?? "");
            }}
          >
            <option value="">Selecciona…</option>
            {TRANSFER_REASONS.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <Field
        label={reason === "other" ? "Nota" : "Nota (opcional)"}
        hint="No escribas diagnósticos, documentos de identidad ni otros datos sensibles"
      >
        {(p) => (
          <Textarea
            {...p}
            required={reason === "other"}
            minLength={3}
            maxLength={200}
            value={note}
            onChange={(e) => {
              onNoteChange(e.target.value);
            }}
          />
        )}
      </Field>
    </>
  );
}
