import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, QueryState } from "@/shared/ui/field";

import { listMessages, retryMessage, type Message } from "../api/generated/notifications";

const TEMPLATES: Readonly<Record<string, string>> = {
  policy_issued: "Póliza expedida",
  installment_due_soon: "Recordatorio de cuota",
  installment_due_today: "Cuota vence hoy",
  installment_overdue: "Cuota vencida",
  policy_in_arrears: "Póliza en mora",
  policy_reinstated: "Póliza al día",
  policy_expiring: "Póliza por vencer",
  claim_reported: "Siniestro reportado",
  claim_status_changed: "Cambio en siniestro",
};
const STATUS: Record<Message["status"], string> = {
  pending: "Pendiente",
  sent: "Enviado",
  failed: "Fallido",
  skipped: "Omitido",
};
const SKIP: Record<NonNullable<Message["skipReason"]>, string> = {
  no_email: "el cliente no tiene correo válido",
  consent_revoked: "el cliente revocó su autorización",
  no_recipient: "sin destinatario",
};

function MessageRow({ m }: { m: Message }) {
  const qc = useQueryClient();
  const canRetry = usePermission(permissions.notificationManage);
  const retry = useMutation({
    mutationFn: async () => {
      const res = await retryMessage(m.id);
      if (res.status !== 204) throw new Error("no se pudo reintentar");
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notifications", "messages"] }),
  });
  return (
    <li className="space-y-1 rounded-md border p-3 text-sm">
      <p>
        {TEMPLATES[m.template] ?? m.template} · {STATUS[m.status]}
        {m.skipReason ? ` · ${SKIP[m.skipReason]}` : ""} · {m.createdAt.slice(0, 10)}
      </p>
      {m.status === "failed" && canRetry && (
        <Button
          variant="outline"
          disabled={retry.isPending}
          onClick={() => {
            retry.mutate();
          }}
        >
          Reintentar
        </Button>
      )}
      <ErrorMessage message={problemMessage(retry.error)} />
    </li>
  );
}

/** Bandeja de salida de los avisos a clientes. */
export function NotificationsPage() {
  const query = useInfiniteQuery({
    queryKey: ["notifications", "messages"],
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const res = await listMessages({ limit: 50, ...(pageParam ? { cursor: pageParam } : {}) });
      if (res.status !== 200) throw new Error("avisos no disponibles");
      return res.data;
    },
    getNextPageParam: (last) => last.nextCursor,
  });
  const items = query.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <section aria-labelledby="notifications-title" className="space-y-4">
      <h1 id="notifications-title" className="text-2xl font-semibold">
        Notificaciones
      </h1>
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={items.length === 0}
        emptyMessage="No hay avisos."
      />
      <ul className="space-y-2">
        {items.map((m) => (
          <MessageRow key={m.id} m={m} />
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
