import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { problemMessage } from "@/shared/lib/problem";
import { Button } from "@/shared/ui/button";
import { requiredReason, type TransferReasonCode } from "@/shared/lib/transferReasons";
import { TransferReasonFields } from "@/shared/ui/TransferReasonFields";
import { ErrorMessage, Field, QueryState, Select } from "@/shared/ui/field";

import { transferPortfolio } from "../api/generated/policies";
import { agentsQuery } from "../queries";

/** Traspaso de toda la cartera de un asesor a otro. Se procesa en segundo plano. */
export function PortfolioTransferPage() {
  const agents = useQuery(agentsQuery());
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [reason, setReason] = useState<TransferReasonCode | "">("");
  const [note, setNote] = useState("");
  const transfer = useMutation({
    mutationFn: () =>
      transferPortfolio({
        fromAgentId: from,
        toAgentId: to,
        reason: requiredReason(reason),
        ...(note ? { note } : {}),
      }),
    onSuccess: () => {
      setFrom("");
      setTo("");
      setReason("");
      setNote("");
    },
  });
  return (
    <section aria-labelledby="transfer-title" className="max-w-xl space-y-4">
      <h1 id="transfer-title" className="text-2xl font-semibold">
        Traspaso de cartera
      </h1>
      <p className="text-sm text-muted-foreground">
        Pasa los clientes y las pólizas no terminadas de un asesor a otro, por ejemplo cuando
        alguien se retira. Las pólizas terminadas conservan su asesor como historial. El traspaso
        queda registrado y puede tardar unos minutos.
      </p>
      <QueryState
        isPending={agents.isPending}
        isError={agents.isError}
        isEmpty={agents.data?.length === 0}
        emptyMessage="La agencia aún no tiene usuarios."
      />
      {transfer.isSuccess && (
        <p role="status" className="rounded border p-3 text-sm">
          Traspaso solicitado. Se está procesando.
        </p>
      )}
      <ErrorMessage message={problemMessage(transfer.error)} />
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          transfer.mutate();
        }}
      >
        <Field label="Asesor actual" hint="Incluye a quienes ya no están activos">
          {(p) => (
            <Select
              {...p}
              required
              value={from}
              onChange={(e) => {
                setFrom(e.target.value);
              }}
            >
              <option value="">Selecciona…</option>
              {agents.data?.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.displayName}
                  {a.active ? "" : " (no activo)"}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field
          label="Asesor que recibe"
          hint="Solo asesores activos, que ya hayan ingresado una vez"
        >
          {(p) => (
            <Select
              {...p}
              required
              value={to}
              onChange={(e) => {
                setTo(e.target.value);
              }}
            >
              <option value="">Selecciona…</option>
              {agents.data
                ?.filter((a) => a.active && a.id !== from)
                .map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.displayName}
                  </option>
                ))}
            </Select>
          )}
        </Field>
        <TransferReasonFields
          reason={reason}
          note={note}
          onReasonChange={setReason}
          onNoteChange={setNote}
        />
        <Button type="submit" disabled={transfer.isPending}>
          Traspasar cartera
        </Button>
      </form>
    </section>
  );
}
