import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type SubmitEvent } from "react";

import { formatDate } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState, Select, Textarea } from "@/shared/ui/field";

import {
  createComplaint,
  exportHabeas,
  getKyc,
  listComplaints,
  listSuppressions,
  readKycDetails,
  recordKyc,
  requestSuppression,
  resolveComplaint,
  resolveSuppression,
  startComplaint,
  type HabeasRecord,
} from "../api/generated/compliance";
import type { ComplianceDeps } from "../deps";

const COMPLAINT_LABELS = {
  received: "Recibida",
  in_progress: "En trámite",
  resolved: "Resuelta",
} as const;
const SUPPRESSION_LABELS = {
  requested: "Solicitada",
  accepted: "Aceptada",
  denied: "Negada",
} as const;

function text(f: FormData, name: string): string {
  const v = f.get(name);
  return typeof v === "string" ? v.trim() : "";
}

function KycDetailsButton({ partyId, checkId }: { partyId: string; checkId: string }) {
  const read = useMutation({
    mutationFn: async () => {
      const res = await readKycDetails(partyId, checkId);
      if (res.status !== 200) throw new Error("detalle no disponible");
      return res.data.details;
    },
  });
  return (
    <>
      {read.data === undefined ? (
        <Button
          variant="outline"
          onClick={() => {
            read.mutate();
          }}
        >
          Ver detalle
        </Button>
      ) : (
        <p className="mt-1 whitespace-pre-wrap">{read.data}</p>
      )}
      <ErrorMessage message={problemMessage(read.error)} />
    </>
  );
}

function KycPanel({ partyId }: { partyId: string }) {
  const qc = useQueryClient();
  const canRecord = usePermission(permissions.kycRecord);
  const canDetails = usePermission(permissions.kycReadDetails);
  const key = ["compliance", "kyc", partyId];
  const status = useQuery({
    queryKey: key,
    queryFn: async () => {
      const res = await getKyc(partyId);
      if (res.status !== 200) throw new Error("verificación no disponible");
      return res.data;
    },
  });
  const record = useMutation({
    mutationFn: async (f: FormData) => {
      const details = text(f, "details");
      const res = await recordKyc(partyId, {
        approved: text(f, "result") === "approved",
        source: text(f, "source"),
        checkedOn: text(f, "checkedOn"),
        ...(details ? { details } : {}),
      });
      if (res.status !== 201) throw new Error("verificación no registrada");
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  });
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    record.mutate(new FormData(e.currentTarget));
  };
  return (
    <div className="space-y-3">
      <h3 className="font-medium">Verificación SARLAFT</h3>
      <QueryState
        isPending={status.isPending}
        isError={status.isError}
        isEmpty={false}
        emptyMessage=""
      />
      {status.data && (
        <>
          <p className="text-sm">
            Estado: {status.data.approved ? "Aprobada" : "Sin aprobación vigente"}
            {status.data.stale && " · el documento cambió después de la última verificación"}
          </p>
          <ul className="space-y-2">
            {status.data.checks.map((c) => (
              <li key={c.id} className="rounded-md border p-2 text-sm">
                {formatDate(c.checkedOn)} · {c.approved ? "Aprobada" : "Rechazada"} · {c.source}
                {c.hasDetails && canDetails && (
                  <div className="mt-1">
                    <KycDetailsButton partyId={partyId} checkId={c.id} />
                  </div>
                )}
              </li>
            ))}
          </ul>
        </>
      )}
      {canRecord && (
        <form onSubmit={submit} className="grid gap-3 rounded-md border p-3 sm:grid-cols-3">
          <Field label="Resultado">
            {(p) => (
              <Select {...p} name="result" required>
                <option value="approved">Aprobada</option>
                <option value="rejected">Rechazada</option>
              </Select>
            )}
          </Field>
          <Field label="Lista o fuente consultada">
            {(p) => <Input {...p} name="source" required minLength={2} maxLength={200} />}
          </Field>
          <Field label="Fecha de consulta">
            {(p) => <Input {...p} name="checkedOn" type="date" required />}
          </Field>
          <div className="sm:col-span-3">
            <Field label="Detalle (protegido)">
              {(p) => <Textarea {...p} name="details" maxLength={4000} />}
            </Field>
          </div>
          <div className="sm:col-span-3">
            <ErrorMessage message={problemMessage(record.error)} />
            <Button type="submit" disabled={record.isPending}>
              Registrar verificación
            </Button>
          </div>
        </form>
      )}
    </div>
  );
}

function HabeasPanel({ partyId }: { partyId: string }) {
  const qc = useQueryClient();
  const canExport = usePermission(permissions.habeasExport);
  const canSuppress = usePermission(permissions.habeasSuppress);
  const [record, setRecord] = useState<HabeasRecord | null>(null);
  const exportData = useMutation({
    mutationFn: async () => {
      const res = await exportHabeas(partyId);
      if (res.status !== 200) throw new Error("exporte no disponible");
      return res.data;
    },
    onSuccess: setRecord,
  });
  const suppress = useMutation({
    mutationFn: async () => {
      const res = await requestSuppression(partyId);
      if (res.status !== 201) throw new Error("solicitud no registrada");
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["compliance", "suppressions"] }),
  });
  if (!canExport && !canSuppress) return null;
  return (
    <div className="space-y-2">
      <h3 className="font-medium">Habeas data</h3>
      <div className="flex flex-wrap gap-2">
        {canExport && (
          <Button
            variant="outline"
            disabled={exportData.isPending}
            onClick={() => {
              exportData.mutate();
            }}
          >
            Consultar datos del titular
          </Button>
        )}
        {canSuppress && (
          <Button
            variant="outline"
            disabled={suppress.isPending}
            onClick={() => {
              suppress.mutate();
            }}
          >
            Registrar solicitud de supresión
          </Button>
        )}
      </div>
      {suppress.isSuccess && (
        <p className="text-sm">Solicitud registrada. No se borró ningún dato.</p>
      )}
      <ErrorMessage message={problemMessage(exportData.error ?? suppress.error)} />
      {record && (
        <dl className="rounded-md border p-3 text-sm">
          <dt className="font-medium">Titular</dt>
          <dd>
            {record.name} ({record.documentType} •••• {record.documentLast4})
          </dd>
          <dt className="mt-2 font-medium">Contacto</dt>
          <dd>
            {[record.email, record.phone, record.address, record.city]
              .filter(Boolean)
              .join(" · ") || "—"}
          </dd>
          <dt className="mt-2 font-medium">Autorizaciones</dt>
          {record.consents.length === 0 && <dd>Sin registros.</dd>}
          {record.consents.map((c) => (
            <dd key={`${c.consentDate}-${c.purpose}-${c.status}`}>
              {c.consentDate} · {c.purpose} · {c.status} · versión {c.textVersion}
            </dd>
          ))}
        </dl>
      )}
    </div>
  );
}

function ComplaintItem({
  c,
}: {
  c: {
    id: string;
    subject: string;
    channel: string;
    description: string;
    status: keyof typeof COMPLAINT_LABELS;
    resolution?: string;
    receivedOn: string;
  };
}) {
  const qc = useQueryClient();
  const done = () => qc.invalidateQueries({ queryKey: ["compliance", "complaints"] });
  const start = useMutation({
    mutationFn: async () => {
      const res = await startComplaint(c.id);
      if (res.status !== 200) throw new Error("no se pudo iniciar");
    },
    onSuccess: done,
  });
  const resolve = useMutation({
    mutationFn: async (resolution: string) => {
      const res = await resolveComplaint(c.id, { resolution });
      if (res.status !== 200) throw new Error("no se pudo resolver");
    },
    onSuccess: done,
  });
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    resolve.mutate(text(new FormData(e.currentTarget), "resolution"));
  };
  return (
    <li className="space-y-2 rounded-md border p-3 text-sm">
      <p className="font-medium">
        {c.subject} · {COMPLAINT_LABELS[c.status]}
      </p>
      <p>
        {formatDate(c.receivedOn)} · {c.channel}
      </p>
      <p className="whitespace-pre-wrap">{c.description}</p>
      {c.resolution && <p className="whitespace-pre-wrap">Respuesta: {c.resolution}</p>}
      {c.status === "received" && (
        <Button
          variant="outline"
          disabled={start.isPending}
          onClick={() => {
            start.mutate();
          }}
        >
          Iniciar trámite
        </Button>
      )}
      {c.status === "in_progress" && (
        <form onSubmit={submit} className="space-y-2">
          <Field label="Respuesta">
            {(p) => <Textarea {...p} name="resolution" required minLength={3} maxLength={4000} />}
          </Field>
          <Button type="submit" disabled={resolve.isPending}>
            Resolver
          </Button>
        </form>
      )}
      <ErrorMessage message={problemMessage(start.error ?? resolve.error)} />
    </li>
  );
}

function ComplaintsSection({ partyId }: { partyId: string }) {
  const qc = useQueryClient();
  const query = useInfiniteQuery({
    queryKey: ["compliance", "complaints"],
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const res = await listComplaints({ limit: 50, ...(pageParam ? { cursor: pageParam } : {}) });
      if (res.status !== 200) throw new Error("quejas no disponibles");
      return res.data;
    },
    getNextPageParam: (last) => last.nextCursor,
  });
  const create = useMutation({
    mutationFn: async (f: FormData) => {
      const res = await createComplaint({
        channel: text(f, "channel"),
        subject: text(f, "subject"),
        description: text(f, "description"),
        receivedOn: text(f, "receivedOn"),
        ...(partyId ? { partyId } : {}),
      });
      if (res.status !== 201) throw new Error("queja no registrada");
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["compliance", "complaints"] }),
  });
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    create.mutate(new FormData(e.currentTarget));
  };
  const items = query.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <section aria-labelledby="pqr-title" className="space-y-3">
      <h2 id="pqr-title" className="text-lg font-medium">
        Quejas y PQR
      </h2>
      <form onSubmit={submit} className="grid gap-3 rounded-md border p-3 sm:grid-cols-3">
        <Field label="Canal">
          {(p) => <Input {...p} name="channel" required minLength={2} maxLength={60} />}
        </Field>
        <Field label="Asunto">
          {(p) => <Input {...p} name="subject" required minLength={3} maxLength={200} />}
        </Field>
        <Field label="Fecha de recibo">
          {(p) => <Input {...p} name="receivedOn" type="date" required />}
        </Field>
        <div className="sm:col-span-3">
          <Field
            label="Descripción"
            hint={partyId ? "Quedará ligada al cliente seleccionado." : "Sin cliente seleccionado."}
          >
            {(p) => <Textarea {...p} name="description" required minLength={3} maxLength={4000} />}
          </Field>
        </div>
        <div className="sm:col-span-3">
          <ErrorMessage message={problemMessage(create.error)} />
          <Button type="submit" disabled={create.isPending}>
            Registrar
          </Button>
        </div>
      </form>
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={items.length === 0}
        emptyMessage="No hay quejas registradas."
      />
      <ul className="space-y-2">
        {items.map((c) => (
          <ComplaintItem key={c.id} c={c} />
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

function SuppressionItem({
  s,
}: {
  s: {
    id: string;
    status: keyof typeof SUPPRESSION_LABELS;
    resolution?: string;
    createdAt: string;
  };
}) {
  const qc = useQueryClient();
  const resolve = useMutation({
    mutationFn: async (input: { accepted: boolean; resolution: string }) => {
      const res = await resolveSuppression(s.id, input);
      if (res.status !== 204) throw new Error("no se pudo decidir");
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["compliance", "suppressions"] }),
  });
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    resolve.mutate({
      accepted: text(f, "decision") === "accepted",
      resolution: text(f, "resolution"),
    });
  };
  return (
    <li className="space-y-2 rounded-md border p-3 text-sm">
      <p>
        {s.createdAt.slice(0, 10)} · {SUPPRESSION_LABELS[s.status]}
        {s.resolution ? ` · ${s.resolution}` : ""}
      </p>
      {s.status === "requested" && (
        <form onSubmit={submit} className="grid gap-2 sm:grid-cols-2">
          <Field label="Decisión">
            {(p) => (
              <Select {...p} name="decision" required>
                <option value="accepted">Aceptar</option>
                <option value="denied">Negar</option>
              </Select>
            )}
          </Field>
          <Field label="Motivo" hint="La decisión no borra datos: la supresión se ejecuta aparte.">
            {(p) => <Input {...p} name="resolution" required minLength={3} maxLength={2000} />}
          </Field>
          <div className="sm:col-span-2">
            <ErrorMessage message={problemMessage(resolve.error)} />
            <Button type="submit" disabled={resolve.isPending}>
              Guardar decisión
            </Button>
          </div>
        </form>
      )}
    </li>
  );
}

function SuppressionsSection() {
  const query = useQuery({
    queryKey: ["compliance", "suppressions"],
    queryFn: async () => {
      const res = await listSuppressions({ limit: 100 });
      if (res.status !== 200) throw new Error("solicitudes no disponibles");
      return res.data.items;
    },
  });
  return (
    <section aria-labelledby="supp-title" className="space-y-3">
      <h2 id="supp-title" className="text-lg font-medium">
        Solicitudes de supresión
      </h2>
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={query.data?.length === 0}
        emptyMessage="No hay solicitudes."
      />
      <ul className="space-y-2">
        {query.data?.map((s) => (
          <SuppressionItem key={s.id} s={s} />
        ))}
      </ul>
    </section>
  );
}

/** Cumplimiento (Flujo 10): SARLAFT con consulta manual, PQR y habeas data. */
export function CompliancePage({ deps }: { deps: ComplianceDeps }) {
  const [partyId, setPartyId] = useState("");
  const canKyc = usePermission(permissions.kycRead);
  const canComplaints = usePermission(permissions.complaintManage);
  const canSuppress = usePermission(permissions.habeasSuppress);
  return (
    <section aria-labelledby="compliance-title" className="space-y-6">
      <h1 id="compliance-title" className="text-2xl font-semibold">
        Cumplimiento
      </h1>
      <section aria-labelledby="party-title" className="space-y-3">
        <h2 id="party-title" className="text-lg font-medium">
          Cliente
        </h2>
        <deps.PartyPicker label="Cliente" value={partyId} onChange={setPartyId} />
        {partyId && canKyc && <KycPanel key={partyId} partyId={partyId} />}
        {partyId && <HabeasPanel key={`h-${partyId}`} partyId={partyId} />}
      </section>
      {canComplaints && <ComplaintsSection partyId={partyId} />}
      {canSuppress && <SuppressionsSection />}
    </section>
  );
}
