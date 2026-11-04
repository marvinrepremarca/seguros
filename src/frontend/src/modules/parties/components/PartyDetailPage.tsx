import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";

import { formatDate, formatDateTime } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState, Select } from "@/shared/ui/field";

import {
  getParty,
  listPartyConsents,
  registerPartyConsent,
  revealPartyDocument,
  updateParty,
  type Party,
} from "../api/generated/parties";
import type { AgentsQuery } from "../deps";
import {
  consentFormSchema,
  partyEditSchema,
  type ConsentFormValues,
  type PartyEditValues,
} from "../schemas";
import { ResponsibleAgentSection } from "./ResponsibleAgentSection";

export function PartyDetailPage({
  partyId,
  agentsQuery,
}: {
  partyId: string;
  agentsQuery: AgentsQuery;
}) {
  const canAssign = usePermission(permissions.partyAssign);
  const party = useQuery({
    queryKey: ["parties", "detail", partyId],
    queryFn: async () => {
      const res = await getParty(partyId);
      if (res.status !== 200) throw new Error("cliente no disponible");
      return res.data;
    },
  });
  if (party.data === undefined) {
    return (
      <QueryState
        isPending={party.isPending}
        isError={party.isError}
        isEmpty={false}
        emptyMessage=""
      />
    );
  }
  return (
    <section aria-labelledby="party-title" className="max-w-2xl space-y-8">
      <h1 id="party-title" className="text-2xl font-semibold">
        {party.data.name}
      </h1>
      <DocumentSection key={party.data.id} party={party.data} />
      <EditSection party={party.data} />
      {canAssign && <ResponsibleAgentSection party={party.data} agentsQuery={agentsQuery} />}
      <ConsentsSection partyId={partyId} />
    </section>
  );
}

function DocumentSection({ party }: { party: Party }) {
  const canReveal = usePermission(permissions.documentReveal);
  const [number, setNumber] = useState<string | null>(null);
  const reveal = useMutation({
    mutationFn: async () => {
      const res = await revealPartyDocument(party.id);
      if (res.status !== 200) throw new Error("no revelado");
      // El número solo vive en este estado: no queda en la caché de la mutación.
      setNumber(res.data.documentNumber);
    },
  });
  // El dato Restringido se oculta solo: no permanece visible en una pantalla desatendida.
  useEffect(() => {
    if (number === null) return undefined;
    const t = setTimeout(() => {
      setNumber(null);
    }, 30_000);
    return () => {
      clearTimeout(t);
    };
  }, [number]);

  return (
    <div className="space-y-2">
      <h2 className="text-lg font-semibold">Documento</h2>
      <p>
        {party.documentType}{" "}
        {number ?? (party.documentLast4 ? `•••• ${party.documentLast4}` : "••••")}
      </p>
      <ErrorMessage message={problemMessage(reveal.error)} />
      {canReveal && number === null && (
        <Button
          variant="outline"
          size="sm"
          disabled={reveal.isPending}
          onClick={() => {
            reveal.mutate();
          }}
        >
          Mostrar documento
        </Button>
      )}
      {number !== null && (
        <p className="text-sm text-muted-foreground">
          Esta consulta queda registrada. El número se oculta en 30 segundos.
        </p>
      )}
    </div>
  );
}

function EditSection({ party }: { party: Party }) {
  const canEditAll = usePermission(permissions.partyUpdate);
  const canEditOwn = usePermission(permissions.partyUpdateOwn);
  const canEdit = canEditAll || canEditOwn;
  const queryClient = useQueryClient();
  const form = useForm({
    resolver: zodResolver(partyEditSchema),
    values: {
      name: party.name,
      email: party.email ?? "",
      phone: party.phone ?? "",
      address: party.address ?? "",
      city: party.city ?? "",
    } satisfies PartyEditValues,
  });
  const update = useMutation({
    mutationFn: async (v: PartyEditValues) => {
      const res = await updateParty(party.id, {
        version: party.version,
        name: v.name,
        ...(v.email ? { email: v.email } : {}),
        ...(v.phone ? { phone: v.phone } : {}),
        ...(v.address ? { address: v.address } : {}),
        ...(v.city ? { city: v.city } : {}),
      });
      if (res.status !== 200) throw new Error("no actualizado");
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["parties"] }),
  });
  const { errors } = form.formState;
  return (
    <form
      noValidate
      className="space-y-4"
      onSubmit={(e) => {
        void form.handleSubmit((v) => {
          update.mutate(v);
        })(e);
      }}
    >
      <h2 className="text-lg font-semibold">Datos del cliente</h2>
      <ErrorMessage message={problemMessage(update.error)} />
      <fieldset disabled={!canEdit} className="grid gap-4 sm:grid-cols-2">
        <Field label="Nombre o razón social" error={errors.name?.message}>
          {(p) => <Input {...p} {...form.register("name")} />}
        </Field>
        <Field label="Correo" error={errors.email?.message}>
          {(p) => <Input {...p} type="email" {...form.register("email")} />}
        </Field>
        <Field label="Teléfono" error={errors.phone?.message}>
          {(p) => <Input {...p} type="tel" {...form.register("phone")} />}
        </Field>
        <Field label="Dirección" error={errors.address?.message}>
          {(p) => <Input {...p} {...form.register("address")} />}
        </Field>
        <Field label="Ciudad" error={errors.city?.message}>
          {(p) => <Input {...p} {...form.register("city")} />}
        </Field>
      </fieldset>
      {canEdit && (
        <Button type="submit" disabled={update.isPending}>
          Guardar cambios
        </Button>
      )}
    </form>
  );
}

const CONSENT_LABELS = { granted: "Autorizó", revoked: "Revocó" } as const;

function ConsentsSection({ partyId }: { partyId: string }) {
  const canRead = usePermission(permissions.consentRead);
  const canCreate = usePermission(permissions.consentCreate);
  const queryClient = useQueryClient();
  const key = ["parties", "consents", partyId];
  const consents = useQuery({
    queryKey: key,
    enabled: canRead,
    queryFn: async () => {
      const res = await listPartyConsents(partyId);
      if (res.status !== 200) throw new Error("autorizaciones no disponibles");
      return res.data.items;
    },
  });
  const form = useForm({
    resolver: zodResolver(consentFormSchema),
    defaultValues: {
      status: "granted",
      purpose: "",
      textVersion: "",
      consentDate: "",
    } satisfies ConsentFormValues,
  });
  const register = useMutation({
    mutationFn: async (v: ConsentFormValues) => {
      const res = await registerPartyConsent(partyId, v);
      if (res.status !== 201) throw new Error("no registrada");
    },
    onSuccess: () => {
      form.reset();
      return queryClient.invalidateQueries({ queryKey: key });
    },
  });
  if (!canRead) return null;
  const { errors } = form.formState;
  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">Autorización de tratamiento de datos</h2>
      <QueryState
        isPending={consents.isPending}
        isError={consents.isError}
        isEmpty={consents.data?.length === 0}
        emptyMessage="Este cliente no tiene autorizaciones registradas."
      />
      {consents.data !== undefined && consents.data.length > 0 && (
        <ul className="space-y-2 text-sm">
          {consents.data.map((c) => (
            <li key={c.id} className="rounded border p-2">
              <strong>{CONSENT_LABELS[c.status]}</strong> el {formatDate(c.consentDate)}:{" "}
              {c.purpose} (texto {c.textVersion}). Registrado el {formatDateTime(c.recordedAt)}.
            </li>
          ))}
        </ul>
      )}
      {canCreate && (
        <form
          noValidate
          className="space-y-4 rounded border p-4"
          onSubmit={(e) => {
            void form.handleSubmit((v) => {
              register.mutate(v);
            })(e);
          }}
        >
          <h3 className="font-medium">Registrar autorización o revocación</h3>
          <p className="text-sm text-muted-foreground">
            Los registros no se editan: una revocación se agrega como un registro nuevo.
          </p>
          <ErrorMessage message={problemMessage(register.error)} />
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Decisión" error={errors.status?.message}>
              {(p) => (
                <Select {...p} {...form.register("status")}>
                  <option value="granted">Autoriza</option>
                  <option value="revoked">Revoca</option>
                </Select>
              )}
            </Field>
            <Field label="Fecha" error={errors.consentDate?.message}>
              {(p) => <Input {...p} type="date" {...form.register("consentDate")} />}
            </Field>
            <Field label="Finalidad" error={errors.purpose?.message}>
              {(p) => <Input {...p} {...form.register("purpose")} />}
            </Field>
            <Field label="Versión del texto" error={errors.textVersion?.message}>
              {(p) => <Input {...p} {...form.register("textVersion")} />}
            </Field>
          </div>
          <Button type="submit" variant="outline" disabled={register.isPending}>
            Registrar
          </Button>
        </form>
      )}
    </div>
  );
}
