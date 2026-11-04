import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";

import { usePermission, permissions } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { Field, Input, QueryState } from "@/shared/ui/field";

import { listParties } from "../api/generated/parties";

const KIND_LABELS = { person: "Persona", organization: "Organización" } as const;

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
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <caption className="sr-only">Clientes de la agencia</caption>
            <thead>
              <tr className="border-b">
                <th scope="col" className="py-2 pr-4">
                  Nombre
                </th>
                <th scope="col" className="py-2 pr-4">
                  Tipo
                </th>
                <th scope="col" className="py-2 pr-4">
                  Documento
                </th>
                <th scope="col" className="py-2">
                  Ciudad
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((p) => (
                <tr key={p.id} className="border-b">
                  <td className="py-2 pr-4">
                    <Link
                      to="/clientes/$partyId"
                      params={{ partyId: p.id }}
                      className="underline underline-offset-2"
                    >
                      {p.name}
                    </Link>
                  </td>
                  <td className="py-2 pr-4">{KIND_LABELS[p.kind]}</td>
                  <td className="py-2 pr-4">
                    {p.documentType} •••• {p.documentLast4}
                  </td>
                  <td className="py-2">{p.city ?? "—"}</td>
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
