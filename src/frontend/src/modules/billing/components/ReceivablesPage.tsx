import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { useState } from "react";

import { formatDate, formatMoney } from "@/shared/lib/format";
import { Button } from "@/shared/ui/button";
import { DataTable } from "@/shared/ui/DataTable";
import { Field, QueryState, Select } from "@/shared/ui/field";

import {
  AgingBucket,
  getReceivablesSummary,
  listReceivables,
  type Receivable,
} from "../api/generated/billing";
import { BUCKET_LABELS } from "../labels";

function parseBucket(value: string): AgingBucket | undefined {
  return Object.values(AgingBucket).find((b) => b === value);
}

const columns = [
  {
    header: "Póliza",
    cell: ({ row }) => (
      <Link
        to="/cartera/$policyId"
        params={{ policyId: row.original.policyId }}
        className="underline underline-offset-2"
      >
        {row.original.policyNumber}
      </Link>
    ),
  },
  { header: "Tomador", cell: ({ row }) => row.original.holderName },
  { header: "Vence", cell: ({ row }) => formatDate(row.original.dueDate) },
  { header: "Días vencida", cell: ({ row }) => Math.max(row.original.daysOverdue, 0) },
  {
    header: "Saldo",
    cell: ({ row }) => formatMoney(row.original.balance, row.original.currency),
  },
] satisfies ColumnDef<Receivable>[];

export function ReceivablesPage() {
  const [bucket, setBucket] = useState<AgingBucket | undefined>(undefined);
  const summary = useQuery({
    queryKey: ["billing", "summary"],
    queryFn: async () => {
      const res = await getReceivablesSummary();
      if (res.status !== 200) throw new Error("resumen no disponible");
      return res.data;
    },
  });
  const query = useInfiniteQuery({
    queryKey: ["billing", "receivables", bucket],
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const res = await listReceivables({
        limit: 50,
        ...(bucket ? { bucket } : {}),
        ...(pageParam ? { cursor: pageParam } : {}),
      });
      if (res.status !== 200) throw new Error("cartera no disponible");
      return res.data;
    },
    getNextPageParam: (last) => last.nextCursor,
  });
  const items = query.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <section aria-labelledby="receivables-title" className="space-y-4">
      <h1 id="receivables-title" className="text-2xl font-semibold">
        Cartera y cobro
      </h1>
      <ul className="flex flex-wrap gap-4 text-sm">
        {summary.data?.map((b) => (
          <li key={`${b.currency}-${b.bucket}`} className="rounded-md border p-3">
            <p className="text-muted-foreground">{BUCKET_LABELS[b.bucket]}</p>
            <p className="font-medium">{formatMoney(b.balance, b.currency)}</p>
            <p className="text-muted-foreground">{b.installments} cuotas</p>
          </li>
        ))}
      </ul>
      <div className="max-w-xs">
        <Field label="Antigüedad">
          {(p) => (
            <Select
              {...p}
              value={bucket ?? ""}
              onChange={(e) => {
                setBucket(parseBucket(e.target.value));
              }}
            >
              <option value="">Todas</option>
              {Object.values(AgingBucket).map((b) => (
                <option key={b} value={b}>
                  {BUCKET_LABELS[b]}
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
        emptyMessage="No hay cuotas con saldo."
      />
      {items.length > 0 && (
        <DataTable
          caption="Cuotas con saldo"
          columns={columns}
          data={items}
          rowKey={(r) => r.installmentId}
        />
      )}
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
