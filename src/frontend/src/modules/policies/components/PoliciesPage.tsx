import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { useState } from "react";

import { formatDate, formatMoney } from "@/shared/lib/format";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { DataTable } from "@/shared/ui/DataTable";
import { Field, QueryState, Select } from "@/shared/ui/field";

import { listPolicies, PolicyStatus, type PolicySummary } from "../api/generated/policies";
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

  const columns = [
    {
      header: "Número",
      cell: ({ row }) => (
        <Link
          to="/polizas/$policyId"
          params={{ policyId: row.original.id }}
          className="underline underline-offset-2"
        >
          {row.original.policyNumber ?? "Sin número"}
        </Link>
      ),
    },
    { header: "Tomador", cell: ({ row }) => row.original.holderName },
    { header: "Aseguradora", cell: ({ row }) => insurerName(row.original.insurerId) },
    { header: "Estado", cell: ({ row }) => STATUS_LABELS[row.original.status] },
    {
      header: "Prima neta",
      cell: ({ row }) => formatMoney(row.original.netPremium, row.original.currency),
    },
    {
      header: "Vigencia",
      cell: ({ row }) =>
        `${formatDate(row.original.effectiveFrom)} a ${formatDate(row.original.effectiveTo)}`,
    },
  ] satisfies ColumnDef<PolicySummary>[];

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
        <DataTable
          caption="Pólizas de la agencia"
          columns={columns}
          data={items}
          rowKey={(p) => p.id}
        />
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
