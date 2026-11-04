import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";

import { formatDate, formatMoney } from "@/shared/lib/format";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { Field, QueryState, Select } from "@/shared/ui/field";

import { listPolicies, PolicyStatus } from "../api/generated/policies";
import type { PolicyDeps } from "../deps";
import { STATUS_LABELS } from "../labels";

function parseStatus(value: string): PolicyStatus | undefined {
  return Object.values(PolicyStatus).find((s) => s === value);
}

export function PoliciesPage({ deps }: { deps: Pick<PolicyDeps, "insurers"> }) {
  const canCreate = usePermission(permissions.policyManage);
  const [status, setStatus] = useState<PolicyStatus | undefined>(undefined);
  const insurers = useQuery(deps.insurers);
  const query = useInfiniteQuery({
    queryKey: ["policies", "list", status],
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const res = await listPolicies({
        limit: 50,
        ...(status ? { status } : {}),
        ...(pageParam ? { cursor: pageParam } : {}),
      });
      if (res.status !== 200) throw new Error("pólizas no disponibles");
      return res.data;
    },
    getNextPageParam: (last) => last.nextCursor,
  });
  const items = query.data?.pages.flatMap((p) => p.items) ?? [];
  const insurerName = (id: string) => insurers.data?.find((i) => i.id === id)?.name ?? "—";

  return (
    <section aria-labelledby="policies-title" className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 id="policies-title" className="text-2xl font-semibold">
          Pólizas
        </h1>
        {canCreate && (
          <Button asChild>
            <Link to="/polizas/nueva">Nueva póliza</Link>
          </Button>
        )}
      </div>
      <div className="max-w-xs">
        <Field label="Estado">
          {(p) => (
            <Select
              {...p}
              value={status ?? ""}
              onChange={(e) => {
                setStatus(parseStatus(e.target.value));
              }}
            >
              <option value="">Todos</option>
              {Object.values(PolicyStatus).map((s) => (
                <option key={s} value={s}>
                  {STATUS_LABELS[s]}
                </option>
              ))}
            </Select>
          )}
        </Field>
      </div>
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={items.length === 0}
        emptyMessage="No hay pólizas para mostrar."
      />
      {items.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <caption className="sr-only">Pólizas de la agencia</caption>
            <thead>
              <tr className="border-b">
                <th scope="col" className="py-2 pr-4">
                  Número
                </th>
                <th scope="col" className="py-2 pr-4">
                  Tomador
                </th>
                <th scope="col" className="py-2 pr-4">
                  Aseguradora
                </th>
                <th scope="col" className="py-2 pr-4">
                  Estado
                </th>
                <th scope="col" className="py-2 pr-4">
                  Prima neta
                </th>
                <th scope="col" className="py-2">
                  Vigencia
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((p) => (
                <tr key={p.id} className="border-b">
                  <td className="py-2 pr-4">
                    <Link
                      to="/polizas/$policyId"
                      params={{ policyId: p.id }}
                      className="underline underline-offset-2"
                    >
                      {p.policyNumber ?? "Sin número"}
                    </Link>
                  </td>
                  <td className="py-2 pr-4">{p.holderName}</td>
                  <td className="py-2 pr-4">{insurerName(p.insurerId)}</td>
                  <td className="py-2 pr-4">{STATUS_LABELS[p.status]}</td>
                  <td className="py-2 pr-4">{formatMoney(p.netPremium, p.currency)}</td>
                  <td className="py-2">
                    {formatDate(p.effectiveFrom)} a {formatDate(p.effectiveTo)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {query.hasNextPage && (
        <Button
          variant="outline"
          disabled={query.isFetchingNextPage}
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
