import { useQuery } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

import { formatMoney } from "@/shared/lib/format";
import { Field, Input, QueryState } from "@/shared/ui/field";

import { getKpis } from "../api/generated/reporting";

const BUCKETS = {
  d0_30: "0 a 30 días",
  d31_60: "31 a 60 días",
  d61_90: "61 a 90 días",
  d90_plus: "Más de 90 días",
} as const;

function Kpi({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="rounded-md border p-3 text-sm">
      <h2 className="font-medium">{title}</h2>
      <div className="mt-1 space-y-1">{children}</div>
    </div>
  );
}

function ratio(part: number, total: number): string {
  return total === 0 ? "Sin datos" : `${((part * 100) / total).toFixed(1)} % (${part} de ${total})`;
}

function Empty() {
  return <p className="text-muted-foreground">Sin datos en el periodo.</p>;
}

function monthStart(today: string): string {
  return `${today.slice(0, 7)}-01`;
}

/** Indicadores de la agencia. */
export function ReportingPage({ today }: { today: string }) {
  const [from, setFrom] = useState(monthStart(today));
  const [to, setTo] = useState(today);
  const query = useQuery({
    queryKey: ["reporting", "kpis", from, to],
    enabled: from !== "" && to !== "",
    queryFn: async () => {
      const res = await getKpis({ from, to });
      if (res.status !== 200) throw new Error("indicadores no disponibles");
      return res.data;
    },
  });
  const k = query.data;
  return (
    <section aria-labelledby="reporting-title" className="space-y-4">
      <h1 id="reporting-title" className="text-2xl font-semibold">
        Reportes
      </h1>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="Desde">
          {(p) => (
            <Input
              {...p}
              type="date"
              value={from}
              onChange={(e) => {
                setFrom(e.target.value);
              }}
            />
          )}
        </Field>
        <Field label="Hasta">
          {(p) => (
            <Input
              {...p}
              type="date"
              value={to}
              onChange={(e) => {
                setTo(e.target.value);
              }}
            />
          )}
        </Field>
      </div>
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={false}
        emptyMessage=""
      />
      {k && (
        <div className="grid gap-3 sm:grid-cols-2">
          <Kpi title="Primas emitidas">
            {k.premiums.length === 0 && <Empty />}
            {k.premiums.map((p) => (
              <p key={p.currency}>{formatMoney(p.amount, p.currency)}</p>
            ))}
          </Kpi>
          <Kpi title="Tiempo de expedición">
            <p>{k.issuanceDays} días en promedio</p>
          </Kpi>
          <Kpi title="Pólizas por cliente">
            <p>
              {k.policiesPerClient.policies} pólizas vigentes de {k.policiesPerClient.clients}{" "}
              clientes
            </p>
          </Kpi>
          {k.conversion && (
            <Kpi title="Conversión cotización a póliza">
              <p>{ratio(k.conversion.accepted, k.conversion.requests)}</p>
            </Kpi>
          )}
          {k.retention && (
            <Kpi title="Retención">
              <p>{ratio(k.retention.renewed, k.retention.due)}</p>
            </Kpi>
          )}
          {k.overdue && (
            <Kpi title="Cartera vencida por edades (hoy)">
              {k.overdue.length === 0 && (
                <p className="text-muted-foreground">Sin cartera vencida.</p>
              )}
              {k.overdue.map((b) => (
                <p key={`${b.bucket}-${b.currency}`}>
                  {BUCKETS[b.bucket]}: {formatMoney(b.balance, b.currency)}
                </p>
              ))}
            </Kpi>
          )}
          {k.commissions && (
            <Kpi title="Comisiones causadas y recibidas">
              {k.commissions.length === 0 && <Empty />}
              {k.commissions.map((c) => (
                <p key={c.currency}>
                  Causadas {formatMoney(c.accrued, c.currency)} · recibidas{" "}
                  {formatMoney(c.received, c.currency)}
                </p>
              ))}
            </Kpi>
          )}
          {k.claimDays !== undefined && (
            <Kpi title="Tiempo de atención de siniestros">
              <p>{k.claimDays} días en promedio</p>
            </Kpi>
          )}
          {k.lossRatio && (
            <Kpi title="Siniestralidad">
              {k.lossRatio.length === 0 && <Empty />}
              {k.lossRatio.map((l) => (
                <p key={l.currency}>
                  {l.percentage === undefined ? "Sin primas" : `${l.percentage} %`} · pagado{" "}
                  {formatMoney(l.paid, l.currency)}
                </p>
              ))}
            </Kpi>
          )}
        </div>
      )}
    </section>
  );
}
