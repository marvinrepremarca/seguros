import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";

import { formatDate } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState, Select } from "@/shared/ui/field";

import {
  CaseStatus,
  listCases,
  markCaseLost,
  startCase,
  type RenewalCase,
} from "../api/generated/renewals";

const STATUS_LABELS: Readonly<Record<CaseStatus, string>> = {
  open: "Por gestionar",
  in_progress: "En gestión",
  renewed: "Renovada",
  lost: "Perdida",
  closed: "Cerrada",
};

function parseStatus(value: string): CaseStatus | undefined {
  return Object.values(CaseStatus).find((s) => s === value);
}

function CaseRow({ item }: { item: RenewalCase }) {
  const canManage = usePermission(permissions.renewalManage);
  const client = useQueryClient();
  const [reason, setReason] = useState("");
  const refresh = () => client.invalidateQueries({ queryKey: ["renewals"] });
  const start = useMutation({
    mutationFn: async () => {
      const res = await startCase(item.policyId);
      if (res.status !== 200) throw new Error("caso no iniciado");
    },
    onSuccess: refresh,
  });
  const lost = useMutation({
    mutationFn: async () => {
      const res = await markCaseLost(item.policyId, { reason });
      if (res.status !== 200) throw new Error("caso no actualizado");
    },
    onSuccess: refresh,
  });
  const active = item.status === "open" || item.status === "in_progress";
  return (
    <li className="space-y-2 rounded-md border p-3 text-sm">
      <p>
        <Link
          to="/polizas/$policyId"
          params={{ policyId: item.policyId }}
          className="underline underline-offset-2"
        >
          {item.policyNumber}
        </Link>{" "}
        · {item.holderName} · vence {formatDate(item.effectiveTo)} · {STATUS_LABELS[item.status]}
      </p>
      {item.lostReason && <p className="text-muted-foreground">Motivo: {item.lostReason}</p>}
      <ErrorMessage message={problemMessage(start.error ?? lost.error)} />
      {canManage && active && (
        <div className="flex flex-wrap items-end gap-2">
          {item.status === "open" && (
            <Button
              variant="outline"
              disabled={start.isPending}
              onClick={() => {
                start.mutate();
              }}
            >
              Iniciar gestión
            </Button>
          )}
          <Field label="Motivo si no renueva">
            {(p) => (
              <Input
                {...p}
                maxLength={500}
                value={reason}
                onChange={(e) => {
                  setReason(e.target.value);
                }}
              />
            )}
          </Field>
          <Button
            variant="destructive"
            disabled={reason.trim().length < 3 || lost.isPending}
            onClick={() => {
              lost.mutate();
            }}
          >
            No renueva
          </Button>
        </div>
      )}
    </li>
  );
}

export function RenewalsPage() {
  const [status, setStatus] = useState<CaseStatus | undefined>(CaseStatus.open);
  const query = useInfiniteQuery({
    queryKey: ["renewals", "cases", status],
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const res = await listCases({
        limit: 50,
        ...(status ? { status } : {}),
        ...(pageParam ? { cursor: pageParam } : {}),
      });
      if (res.status !== 200) throw new Error("casos no disponibles");
      return res.data;
    },
    getNextPageParam: (last) => last.nextCursor,
  });
  const items = query.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <section aria-labelledby="renewals-title" className="space-y-4">
      <h1 id="renewals-title" className="text-2xl font-semibold">
        Renovaciones
      </h1>
      <p className="text-sm text-muted-foreground">
        La nueva vigencia se registra desde el detalle de la póliza, también antes de que venza.
      </p>
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
              {Object.values(CaseStatus).map((s) => (
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
        emptyMessage="No hay casos en este estado."
      />
      <ul className="space-y-2">
        {items.map((c) => (
          <CaseRow key={c.id} item={c} />
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
