import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { useState } from "react";

import { usePermission, permissions } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { DataTable } from "@/shared/ui/DataTable";
import { Field, Input, QueryState } from "@/shared/ui/field";

import { listParties, type Party } from "../api/generated/parties";

const KIND_LABELS = { person: "Persona", organization: "Organización" } as const;

const COLUMNS = [
  {
    header: "Nombre",
    cell: ({ row }) => (
      <Link
        to="/clientes/$partyId"
        params={{ partyId: row.original.id }}
        className="underline underline-offset-2"
      >
        {row.original.name}
      </Link>
    ),
  },
  { header: "Tipo", cell: ({ row }) => KIND_LABELS[row.original.kind] },
  {
    header: "Documento",
    cell: ({ row }) => `${row.original.documentType} •••• ${row.original.documentLast4}`,
  },
  { header: "Ciudad", cell: ({ row }) => row.original.city ?? "—" },
] satisfies ColumnDef<Party>[];

export function PartiesPage() {
  const canCreate = usePermission(permissions.partyCreate);
  const [search, setSearch] = useState("");
  const q = search.trim();
  const query = useInfiniteQuery({
    queryKey: ["parties", "list", q],
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const res = await listParties({
        limit: 50,
        ...(q.length >= 2 ? { q } : {}),
        ...(pageParam ? { cursor: pageParam } : {}),
      });
      if (res.status !== 200) throw new Error("clientes no disponibles");
      return res.data;
    },
    getNextPageParam: (last) => last.nextCursor,
  });
  const items = query.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <section aria-labelledby="parties-title" className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 id="parties-title" className="text-2xl font-semibold">
          Clientes
        </h1>
        {canCreate && (
          <Button asChild>
            <Link to="/clientes/nuevo">Nuevo cliente</Link>
          </Button>
        )}
      </div>
      <div className="max-w-sm">
        <Field label="Buscar por nombre" hint="Escribe al menos 2 letras">
          {(props) => (
            <Input
              {...props}
              type="search"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
              }}
            />
          )}
        </Field>
      </div>
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={items.length === 0}
        emptyMessage="No hay clientes para mostrar."
      />
      {items.length > 0 && (
        <DataTable
          caption="Clientes de la agencia"
          columns={COLUMNS}
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
