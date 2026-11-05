import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { formatDate, formatMoney } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState, Textarea } from "@/shared/ui/field";

import type { AccrualKind } from "../api/generated/commissions";
import { listAccruals, listDiscrepancies, registerStatement } from "../api/generated/commissions";
import { parseLines } from "../lines";

const KIND_LABELS: Readonly<Record<AccrualKind, string>> = {
  payment: "Prima pagada",
  payment_reversal: "Reverso de pago",
  written: "Prima emitida",
  endorsement: "Endoso",
  clawback: "Devolución por cancelación",
};

function Accruals() {
  const query = useQuery({
    queryKey: ["commissions", "accruals"],
    queryFn: async () => {
      const res = await listAccruals({ limit: 200 });
      if (res.status !== 200) throw new Error("comisiones no disponibles");
      return res.data;
    },
  });
  const items = query.data?.items ?? [];
  return (
    <>
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={items.length === 0}
        emptyMessage="Aún no hay comisión causada."
      />
      <ul className="space-y-2 text-sm">
        {items.map((a) => (
          <li key={a.id} className="rounded-md border p-3">
            {a.policyNumber} · {KIND_LABELS[a.kind]} · {formatDate(a.accruedOn)} ·{" "}
            {formatMoney(a.amount, a.currency)} · {a.reconciled ? "Conciliada" : "Por conciliar"}
            {a.missingAgreement ? " · Sin acuerdo de comisión" : ""}
          </li>
        ))}
      </ul>
    </>
  );
}

function Discrepancies() {
  const query = useQuery({
    queryKey: ["commissions", "discrepancies"],
    queryFn: async () => {
      const res = await listDiscrepancies({ limit: 200 });
      if (res.status !== 200) throw new Error("diferencias no disponibles");
      return res.data;
    },
  });
  const items = query.data?.items ?? [];
  return (
    <>
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={items.length === 0}
        emptyMessage="No hay diferencias abiertas."
      />
      <ul className="space-y-2 text-sm">
        {items.map((d) => (
          <li key={d.id} className="rounded-md border p-3">
            {d.policyNumber} · liquidación {d.statementReference} · liquidado {d.reported} ·
            esperado {d.expected}
          </li>
        ))}
      </ul>
    </>
  );
}

function StatementForm() {
  const client = useQueryClient();
  const [insurerId, setInsurerId] = useState("");
  const [reference, setReference] = useState("");
  const [receivedOn, setReceivedOn] = useState("");
  const [text, setText] = useState("");
  const save = useMutation({
    mutationFn: async () => {
      const res = await registerStatement({
        insurerId,
        reference,
        receivedOn,
        lines: parseLines(text),
      });
      if (res.status !== 201) throw new Error("liquidación no registrada");
      return res.data;
    },
    onSuccess: () => client.invalidateQueries({ queryKey: ["commissions"] }),
  });
  return (
    <form
      className="max-w-xl space-y-3"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <Field label="ID de la aseguradora">
        {(p) => (
          <Input
            {...p}
            required
            value={insurerId}
            onChange={(e) => {
              setInsurerId(e.target.value);
            }}
          />
        )}
      </Field>
      <Field label="Referencia de la liquidación">
        {(p) => (
          <Input
            {...p}
            required
            maxLength={100}
            value={reference}
            onChange={(e) => {
              setReference(e.target.value);
            }}
          />
        )}
      </Field>
      <Field label="Fecha de recibo">
        {(p) => (
          <Input
            {...p}
            required
            type="date"
            value={receivedOn}
            onChange={(e) => {
              setReceivedOn(e.target.value);
            }}
          />
        )}
      </Field>
      <Field label="Líneas" hint="Una por línea: número de póliza;valor">
        {(p) => (
          <Textarea
            {...p}
            required
            value={text}
            onChange={(e) => {
              setText(e.target.value);
            }}
          />
        )}
      </Field>
      <ErrorMessage message={problemMessage(save.error)} />
      {save.data && (
        <p role="status">
          {save.data.matched} conciliadas · {save.data.discrepancies} con diferencia
        </p>
      )}
      <Button type="submit" disabled={save.isPending}>
        Registrar y conciliar
      </Button>
    </form>
  );
}

export function CommissionsPage() {
  const canRegister = usePermission(permissions.commissionStatementManage);
  return (
    <section aria-labelledby="commissions-title" className="space-y-6">
      <h1 id="commissions-title" className="text-2xl font-semibold">
        Comisiones
      </h1>
      {canRegister && (
        <div className="space-y-2">
          <h2 className="text-lg font-medium">Liquidación de la aseguradora</h2>
          <StatementForm />
        </div>
      )}
      <div className="space-y-2">
        <h2 className="text-lg font-medium">Diferencias</h2>
        <Discrepancies />
      </div>
      <div className="space-y-2">
        <h2 className="text-lg font-medium">Comisión causada</h2>
        <Accruals />
      </div>
    </section>
  );
}
