import { useMutation, useQuery } from "@tanstack/react-query";
import type { SubmitEvent } from "react";

import { formatMoney } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState } from "@/shared/ui/field";

import {
  listConnectors,
  requestConnectorQuote,
  type Connector,
} from "../api/generated/integrations";

function text(f: FormData, name: string): string {
  const v = f.get(name);
  return typeof v === "string" ? v.trim() : "";
}

function ConnectorCard({ c }: { c: Connector }) {
  const canQuote = usePermission(permissions.integrationQuote);
  const quote = useMutation({
    mutationFn: async (f: FormData) => {
      const res = await requestConnectorQuote(c.key, {
        lineOfBusiness: text(f, "line"),
        sumInsured: text(f, "sum"),
      });
      if (res.status !== 200) throw new Error("consulta no disponible");
      return res.data;
    },
  });
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    quote.mutate(new FormData(e.currentTarget));
  };
  return (
    <li className="space-y-3 rounded-md border p-3 text-sm">
      <p className="font-medium">
        {c.name} · {c.enabled ? "Activo" : "Apagado"}
        {c.simulated && " · Simulado"}
      </p>
      {canQuote && c.enabled && (
        <form onSubmit={submit} className="grid gap-3 sm:grid-cols-2">
          <Field label="Ramo">
            {(p) => <Input {...p} name="line" required minLength={2} maxLength={60} />}
          </Field>
          <Field label="Suma asegurada">
            {(p) => <Input {...p} name="sum" required inputMode="decimal" maxLength={24} />}
          </Field>
          <div className="sm:col-span-2">
            <Button type="submit" disabled={quote.isPending}>
              Consultar prima
            </Button>
          </div>
        </form>
      )}
      <ErrorMessage message={problemMessage(quote.error)} />
      {quote.data && (
        <p role="status">
          {quote.data.simulated && "Valor de prueba, no es una oferta: "}
          {formatMoney(quote.data.netPremium, quote.data.currency)} · {quote.data.reference}
        </p>
      )}
    </li>
  );
}

/** Conectores con aseguradoras. */
export function IntegrationsPage() {
  const query = useQuery({
    queryKey: ["integrations", "connectors"],
    queryFn: async () => {
      const res = await listConnectors();
      if (res.status !== 200) throw new Error("conectores no disponibles");
      return res.data;
    },
  });
  return (
    <section aria-labelledby="integrations-title" className="space-y-4">
      <h1 id="integrations-title" className="text-2xl font-semibold">
        Integraciones
      </h1>
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={query.data?.length === 0}
        emptyMessage="No hay conectores configurados."
      />
      <ul className="space-y-2">
        {query.data?.map((c) => (
          <ConnectorCard key={c.key} c={c} />
        ))}
      </ul>
    </section>
  );
}
