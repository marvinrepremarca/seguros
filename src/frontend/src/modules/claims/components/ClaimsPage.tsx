import { useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import type { SubmitEvent } from "react";

import { formatDate } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState, Select, Textarea } from "@/shared/ui/field";

import { listClaims, reportClaim } from "../api/generated/claims";
import type { ClaimsDeps } from "../deps";
import { CLAIM_LABELS } from "../labels";

function text(f: FormData, name: string): string {
  const v = f.get(name);
  return typeof v === "string" ? v.trim() : "";
}

function ReportForm({ deps }: { deps: ClaimsDeps }) {
  const navigate = useNavigate();
  const policies = useQuery(deps.policies);
  const report = useMutation({
    mutationFn: async (f: FormData) => {
      const claimedAmount = text(f, "claimedAmount");
      const res = await reportClaim({
        policyId: text(f, "policyId"),
        occurredOn: text(f, "occurredOn"),
        location: text(f, "location"),
        claimType: text(f, "claimType"),
        description: text(f, "description"),
        ...(claimedAmount ? { claimedAmount } : {}),
      });
      if (res.status !== 201) throw new Error("siniestro no reportado");
      return res.data;
    },
    onSuccess: (c) => navigate({ to: "/siniestros/$claimId", params: { claimId: c.id } }),
  });
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    report.mutate(new FormData(e.currentTarget));
  };
  return (
    <form onSubmit={submit} className="grid gap-3 rounded-md border p-3 sm:grid-cols-2">
      <h2 className="text-lg font-medium sm:col-span-2">Reportar siniestro</h2>
      <Field label="Póliza">
        {(p) => (
          <Select {...p} name="policyId" required>
            <option value="">Seleccione</option>
            {policies.data?.map((x) => (
              <option key={x.id} value={x.id}>
                {x.label}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <Field label="Fecha de ocurrencia">
        {(p) => <Input {...p} name="occurredOn" type="date" required />}
      </Field>
      <Field label="Lugar">
        {(p) => <Input {...p} name="location" required maxLength={200} />}
      </Field>
      <Field label="Tipo">
        {(p) => <Input {...p} name="claimType" required maxLength={100} />}
      </Field>
      <Field label="Valor reclamado">
        {(p) => <Input {...p} name="claimedAmount" inputMode="decimal" />}
      </Field>
      <div className="sm:col-span-2">
        <Field
          label="Descripción"
          hint="No incluya datos de salud: tienen un campo protegido en el siniestro."
        >
          {(p) => <Textarea {...p} name="description" required maxLength={2000} />}
        </Field>
      </div>
      <div className="sm:col-span-2">
        <ErrorMessage message={problemMessage(report.error)} />
        <Button type="submit" disabled={report.isPending}>
          Reportar
        </Button>
      </div>
    </form>
  );
}

/** Siniestros (Flujo 7). */
export function ClaimsPage({ deps }: { deps: ClaimsDeps }) {
  const canReport = usePermission(permissions.claimReport);
  const query = useInfiniteQuery({
    queryKey: ["claims", "list"],
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const res = await listClaims({ limit: 50, ...(pageParam ? { cursor: pageParam } : {}) });
      if (res.status !== 200) throw new Error("siniestros no disponibles");
      return res.data;
    },
    getNextPageParam: (last) => last.nextCursor,
  });
  const items = query.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <section aria-labelledby="claims-title" className="space-y-4">
      <h1 id="claims-title" className="text-2xl font-semibold">
        Siniestros
      </h1>
      {canReport && <ReportForm deps={deps} />}
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={items.length === 0}
        emptyMessage="No hay siniestros."
      />
      <ul className="space-y-2">
        {items.map((c) => (
          <li key={c.id} className="rounded-md border p-3 text-sm">
            <Link
              to="/siniestros/$claimId"
              params={{ claimId: c.id }}
              className="underline underline-offset-2"
            >
              {c.holderName} · {c.claimType}
            </Link>{" "}
            · {formatDate(c.occurredOn)} · {CLAIM_LABELS[c.status]}
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
