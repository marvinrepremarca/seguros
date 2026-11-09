import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState, type SubmitEvent } from "react";

import { formatDate } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState, Select } from "@/shared/ui/field";

import {
  assignLead,
  createLead,
  LeadSource,
  LeadStatus,
  listLeads,
  markLeadContacted,
  markLeadInterested,
  markLeadLost,
  scheduleLeadFollowUp,
  startLeadQuoting,
  type Lead,
} from "../api/generated/crm";
import type { CrmDeps } from "../deps";

const STATUS_LABELS: Readonly<Record<LeadStatus, string>> = {
  new: "Nuevo",
  contacted: "Contactado",
  interested: "Interesado",
  quoting: "En cotización",
  won: "Ganado",
  lost: "Perdido",
  follow_up: "Seguimiento",
};

const SOURCE_LABELS: Readonly<Record<LeadSource, string>> = {
  referral: "Referido",
  social: "Redes sociales",
  website: "Sitio web",
  partner: "Alianza",
  cross_sell: "Cartera existente",
  call: "Llamada",
  event: "Evento",
  other: "Otro",
};

function text(f: FormData, name: string): string {
  const v = f.get(name);
  return typeof v === "string" ? v.trim() : "";
}

function CreateLeadForm() {
  const client = useQueryClient();
  const create = useMutation({
    mutationFn: async (f: FormData) => {
      const email = text(f, "email");
      const phone = text(f, "phone");
      const res = await createLead({
        name: text(f, "name"),
        source: Object.values(LeadSource).find((x) => x === f.get("source")) ?? LeadSource.other,
        ...(email ? { email } : {}),
        ...(phone ? { phone } : {}),
      });
      if (res.status !== 201) throw new Error("prospecto no creado");
    },
    onSuccess: () => client.invalidateQueries({ queryKey: ["crm"] }),
  });
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    create.mutate(new FormData(form), {
      onSuccess: () => {
        form.reset();
      },
    });
  };
  return (
    <form onSubmit={submit} className="grid gap-3 rounded-md border p-3 sm:grid-cols-2">
      <h2 className="text-lg font-medium sm:col-span-2">Nuevo prospecto</h2>
      <Field label="Nombre">{(p) => <Input {...p} name="name" required maxLength={200} />}</Field>
      <Field label="Origen">
        {(p) => (
          <Select {...p} name="source">
            {Object.values(LeadSource).map((s) => (
              <option key={s} value={s}>
                {SOURCE_LABELS[s]}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <Field label="Teléfono">
        {(p) => <Input {...p} name="phone" type="tel" maxLength={30} />}
      </Field>
      <Field label="Correo">
        {(p) => <Input {...p} name="email" type="email" maxLength={254} />}
      </Field>
      <div className="sm:col-span-2">
        <ErrorMessage message={problemMessage(create.error)} />
        <Button type="submit" disabled={create.isPending}>
          Guardar prospecto
        </Button>
      </div>
    </form>
  );
}

function LeadActions({ lead, deps }: { lead: Lead; deps: CrmDeps }) {
  const client = useQueryClient();
  const [reason, setReason] = useState("");
  const [followUp, setFollowUp] = useState("");
  const [party, setParty] = useState("");
  const act = useMutation({
    mutationFn: async (call: () => Promise<{ status: number }>) => {
      const res = await call();
      if (res.status !== 200) throw new Error("prospecto no actualizado");
    },
    onSuccess: () => client.invalidateQueries({ queryKey: ["crm"] }),
  });
  const s = lead.status;
  const canContact = s === "new" || s === "follow_up";
  const canInterest = s === "contacted" || s === "follow_up";
  const canFollow = s === "contacted" || s === "interested" || s === "quoting";
  return (
    <div className="space-y-2">
      <ErrorMessage message={problemMessage(act.error)} />
      <div className="flex flex-wrap items-end gap-2">
        {canContact && (
          <Button
            variant="outline"
            onClick={() => {
              act.mutate(() => markLeadContacted(lead.id));
            }}
          >
            Marcar contactado
          </Button>
        )}
        {canInterest && (
          <Button
            variant="outline"
            onClick={() => {
              act.mutate(() => markLeadInterested(lead.id));
            }}
          >
            Marcar interesado
          </Button>
        )}
        {canFollow && (
          <>
            <Field label="Fecha de seguimiento">
              {(p) => (
                <Input
                  {...p}
                  type="date"
                  value={followUp}
                  onChange={(e) => {
                    setFollowUp(e.target.value);
                  }}
                />
              )}
            </Field>
            <Button
              variant="outline"
              disabled={!followUp}
              onClick={() => {
                act.mutate(() => scheduleLeadFollowUp(lead.id, { followUpOn: followUp }));
              }}
            >
              Programar seguimiento
            </Button>
          </>
        )}
        <Field label="Motivo de pérdida">
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
          disabled={reason.trim().length < 3}
          onClick={() => {
            act.mutate(() => markLeadLost(lead.id, { reason }));
          }}
        >
          Marcar perdido
        </Button>
      </div>
      {s === "interested" && (
        <div className="space-y-2">
          <deps.PartyPicker label="Cliente" value={party} onChange={setParty} />
          <p className="text-sm text-muted-foreground">
            Si aún no existe, regístrelo en{" "}
            <Link to="/clientes/nuevo" className="underline">
              Clientes
            </Link>
            .
          </p>
          <Button
            disabled={!party}
            onClick={() => {
              act.mutate(() => startLeadQuoting(lead.id, { partyId: party }));
            }}
          >
            Pasar a cotización
          </Button>
        </div>
      )}
    </div>
  );
}

function AssignLead({ lead, deps }: { lead: Lead; deps: CrmDeps }) {
  const client = useQueryClient();
  const agents = useQuery(deps.agents);
  const assign = useMutation({
    mutationFn: async (agentId: string) => {
      const res = await assignLead(lead.id, { agentId });
      if (res.status !== 200) throw new Error("no asignado");
    },
    onSuccess: () => client.invalidateQueries({ queryKey: ["crm"] }),
  });
  return (
    <div className="max-w-xs">
      <Field label="Asesor" error={problemMessage(assign.error) ?? undefined}>
        {(p) => (
          <Select
            {...p}
            value={lead.agentId ?? ""}
            onChange={(e) => {
              assign.mutate(e.target.value);
            }}
          >
            <option value="" disabled>
              Sin asignar
            </option>
            {agents.data
              ?.filter((a) => a.active)
              .map((a) => (
                <option key={a.id} value={a.id}>
                  {a.displayName}
                </option>
              ))}
          </Select>
        )}
      </Field>
    </div>
  );
}

function LeadRow({ lead, deps }: { lead: Lead; deps: CrmDeps }) {
  const canManage = usePermission(permissions.leadManageOwn);
  const canAssign = usePermission(permissions.leadAssign);
  const open = lead.status !== "won" && lead.status !== "lost";
  return (
    <li className="space-y-2 rounded-md border p-3 text-sm">
      <p className="font-medium">
        {lead.name} · {STATUS_LABELS[lead.status]}
      </p>
      <p className="text-muted-foreground">
        {[lead.phone, lead.email, SOURCE_LABELS[lead.source]].filter(Boolean).join(" · ")}
        {lead.followUpOn && ` · seguimiento ${formatDate(lead.followUpOn)}`}
      </p>
      {lead.lostReason && <p>Motivo: {lead.lostReason}</p>}
      {canManage && lead.status === "quoting" && lead.partyId && (
        <Link
          to="/cotizaciones"
          search={{ leadId: lead.id, partyId: lead.partyId }}
          className="underline"
        >
          Crear cotización
        </Link>
      )}
      {canManage && open && <LeadActions lead={lead} deps={deps} />}
      {canAssign && open && <AssignLead lead={lead} deps={deps} />}
    </li>
  );
}

function parseStatus(value: string): LeadStatus | undefined {
  return Object.values(LeadStatus).find((s) => s === value);
}

/** Embudo de prospectos (Flujo 1). */
export function LeadsPage({ deps }: { deps: CrmDeps }) {
  const canCreate = usePermission(permissions.leadCreate);
  const [status, setStatus] = useState<LeadStatus | undefined>();
  const query = useInfiniteQuery({
    queryKey: ["crm", "leads", status],
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const res = await listLeads({
        limit: 50,
        ...(status ? { status } : {}),
        ...(pageParam ? { cursor: pageParam } : {}),
      });
      if (res.status !== 200) throw new Error("prospectos no disponibles");
      return res.data;
    },
    getNextPageParam: (last) => last.nextCursor,
  });
  const items = query.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <section aria-labelledby="leads-title" className="space-y-4">
      <h1 id="leads-title" className="text-2xl font-semibold">
        Prospectos
      </h1>
      {canCreate && <CreateLeadForm />}
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
              {Object.values(LeadStatus).map((s) => (
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
        emptyMessage="No hay prospectos."
      />
      <ul className="space-y-2">
        {items.map((l) => (
          <LeadRow key={l.id} lead={l} deps={deps} />
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
