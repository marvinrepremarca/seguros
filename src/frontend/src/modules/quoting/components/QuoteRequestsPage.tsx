import { useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useState, type SubmitEvent } from "react";

import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState, Select } from "@/shared/ui/field";

import { createQuoteRequest, listQuoteRequests } from "../api/generated/quoting";
import type { QuotingDeps } from "../deps";
import { REQUEST_LABELS } from "../labels";

function NewRequest({
  deps,
  leadId,
  partyId,
}: {
  deps: QuotingDeps;
  leadId?: string;
  partyId?: string;
}) {
  const navigate = useNavigate();
  const lines = useQuery(deps.lines);
  const schemas = useQuery(deps.riskSchemas);
  const [holder, setHolder] = useState(partyId ?? "");
  const [line, setLine] = useState("");
  const [risk, setRisk] = useState<Record<string, string>>({});
  const key = lines.data?.find((l) => l.id === line)?.riskSchemaKey;
  const fields = deps.riskFields(schemas.data?.find((s) => s.key === key)?.schema);
  const create = useMutation({
    mutationFn: async (description: string) => {
      const res = await createQuoteRequest({
        holderPartyId: holder,
        lineOfBusinessId: line,
        riskDescription: description,
        riskData: deps.toRiskData(fields, risk),
        ...(leadId ? { leadId } : {}),
      });
      if (res.status !== 201) throw new Error("solicitud no creada");
      return res.data;
    },
    onSuccess: (r) => navigate({ to: "/cotizaciones/$requestId", params: { requestId: r.id } }),
  });
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    const description = new FormData(e.currentTarget).get("description");
    create.mutate(typeof description === "string" ? description : "");
  };
  return (
    <form onSubmit={submit} className="space-y-3 rounded-md border p-3">
      <h2 className="text-lg font-medium">Nueva solicitud</h2>
      <deps.PartyPicker label="Cliente" value={holder} onChange={setHolder} />
      <Field label="Ramo">
        {(p) => (
          <Select
            {...p}
            required
            value={line}
            onChange={(e) => {
              setLine(e.target.value);
              setRisk({});
            }}
          >
            <option value="">Seleccione</option>
            {lines.data
              ?.filter((l) => l.active)
              .map((l) => (
                <option key={l.id} value={l.id}>
                  {l.name}
                </option>
              ))}
          </Select>
        )}
      </Field>
      <Field label="Qué se quiere proteger">
        {(p) => <Input {...p} name="description" required minLength={2} maxLength={200} />}
      </Field>
      <deps.RiskFields
        fields={fields}
        values={risk}
        onChange={(name, value) => {
          setRisk({ ...risk, [name]: value });
        }}
      />
      <ErrorMessage message={problemMessage(create.error)} />
      <Button type="submit" disabled={!holder || !line || create.isPending}>
        Crear solicitud
      </Button>
    </form>
  );
}

/** Solicitudes de cotización (Flujo 2). */
export function QuoteRequestsPage({
  deps,
  leadId,
  partyId,
}: {
  deps: QuotingDeps;
  leadId?: string | undefined;
  partyId?: string | undefined;
}) {
  const canManage = usePermission(permissions.quoteManageOwn);
  const query = useInfiniteQuery({
    queryKey: ["quoting", "requests"],
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const res = await listQuoteRequests({
        limit: 50,
        ...(pageParam ? { cursor: pageParam } : {}),
      });
      if (res.status !== 200) throw new Error("solicitudes no disponibles");
      return res.data;
    },
    getNextPageParam: (last) => last.nextCursor,
  });
  const items = query.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <section aria-labelledby="quotes-title" className="space-y-4">
      <h1 id="quotes-title" className="text-2xl font-semibold">
        Cotizaciones
      </h1>
      {canManage && (
        <NewRequest deps={deps} {...(leadId ? { leadId } : {})} {...(partyId ? { partyId } : {})} />
      )}
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={items.length === 0}
        emptyMessage="No hay solicitudes."
      />
      <ul className="space-y-2">
        {items.map((r) => (
          <li key={r.id} className="rounded-md border p-3 text-sm">
            <Link
              to="/cotizaciones/$requestId"
              params={{ requestId: r.id }}
              className="underline underline-offset-2"
            >
              {r.holderName} · {r.riskDescription}
            </Link>{" "}
            · {REQUEST_LABELS[r.status]}
          </li>
        ))}
      </ul>
      {query.hasNextPage && (
        <Button
          variant="outline"
          onClick={() => {
            void query.fetchNextPage();
          }}
        >
          Ver más
        </Button>
      )}
    </section>
  );
}
