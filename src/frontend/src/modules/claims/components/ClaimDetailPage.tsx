import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { formatDate, formatMoney } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState, Textarea } from "@/shared/ui/field";

import {
  addChecklistItem,
  addClaimNote,
  advanceClaim,
  AdvanceClaimTo,
  getClaim,
  getClaimHealth,
  getClaimTracking,
  overrideClaimPayment,
  recordClaimPayment,
  setChecklistItem,
  setClaimedAmount,
  setClaimHealth,
  type Claim,
  type ClaimStatus,
} from "../api/generated/claims";
import { CLAIM_LABELS } from "../labels";

// Máquina de estados de DOMAIN.md: la UI solo ofrece lo válido; el backend la vuelve a exigir.
const NEXT: Readonly<Record<ClaimStatus, readonly AdvanceClaimTo[]>> = {
  reported: [AdvanceClaimTo.notified],
  notified: [AdvanceClaimTo.documenting],
  documenting: [AdvanceClaimTo.evaluating],
  evaluating: [AdvanceClaimTo.approved, AdvanceClaimTo.objected],
  approved: [],
  objected: [AdvanceClaimTo.evaluating, AdvanceClaimTo.closed],
  paid: [AdvanceClaimTo.closed],
  closed: [],
};

function useRefresh() {
  const client = useQueryClient();
  return () => client.invalidateQueries({ queryKey: ["claims"] });
}

function Workflow({ claim }: { claim: Claim }) {
  const refresh = useRefresh();
  const [support, setSupport] = useState("");
  const [number, setNumber] = useState("");
  const [amount, setAmount] = useState("");
  const [reason, setReason] = useState("");
  const act = useMutation({
    mutationFn: async (call: () => Promise<{ status: number }>) => {
      const res = await call();
      if (res.status !== 200) throw new Error("siniestro no actualizado");
    },
    onSuccess: refresh,
  });
  const next = NEXT[claim.status];
  const decides = next.includes(AdvanceClaimTo.approved);
  const beforeDecision = ["reported", "notified", "documenting", "evaluating"].includes(
    claim.status,
  );
  return (
    <div className="space-y-3 rounded-md border p-3">
      <h2 className="text-lg font-medium">Gestión</h2>
      <ErrorMessage message={problemMessage(act.error)} />
      {claim.status === "reported" && (
        <Field label="Número de siniestro de la aseguradora">
          {(p) => (
            <Input
              {...p}
              maxLength={60}
              value={number}
              onChange={(e) => {
                setNumber(e.target.value);
              }}
            />
          )}
        </Field>
      )}
      {decides && (
        <Field
          label="Soporte de la decisión de la aseguradora"
          hint="La agencia registra; no decide."
        >
          {(p) => (
            <Input
              {...p}
              maxLength={200}
              value={support}
              onChange={(e) => {
                setSupport(e.target.value);
              }}
            />
          )}
        </Field>
      )}
      <div className="flex flex-wrap gap-2">
        {next.map((to) => (
          <Button
            key={to}
            variant="outline"
            onClick={() => {
              act.mutate(() =>
                advanceClaim(claim.id, {
                  to,
                  ...(number ? { insurerClaimNumber: number } : {}),
                  ...(support ? { support } : {}),
                }),
              );
            }}
          >
            Pasar a: {CLAIM_LABELS[to]}
          </Button>
        ))}
      </div>
      {(beforeDecision || claim.status === "approved") && (
        <div className="flex max-w-lg flex-wrap items-end gap-2">
          <Field label={beforeDecision ? "Valor reclamado" : "Valor pagado"}>
            {(p) => (
              <Input
                {...p}
                inputMode="decimal"
                value={amount}
                onChange={(e) => {
                  setAmount(e.target.value);
                }}
              />
            )}
          </Field>
          {beforeDecision && (
            <Field label="Motivo del valor reclamado">
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
          )}
          <Button
            variant="outline"
            disabled={!amount || (beforeDecision && reason.trim().length < 3)}
            onClick={() => {
              act.mutate(() =>
                beforeDecision
                  ? setClaimedAmount(claim.id, { amount, reason })
                  : recordClaimPayment(claim.id, { amount }),
              );
            }}
          >
            {beforeDecision ? "Guardar valor reclamado" : "Registrar pago"}
          </Button>
        </div>
      )}
    </div>
  );
}

function Override({ claim }: { claim: Claim }) {
  const refresh = useRefresh();
  const [amount, setAmount] = useState("");
  const [reason, setReason] = useState("");
  const [support, setSupport] = useState("");
  const pay = useMutation({
    mutationFn: async () => {
      const res = await overrideClaimPayment(claim.id, { amount, reason, support });
      if (res.status !== 200) throw new Error("pago no registrado");
    },
    onSuccess: refresh,
  });
  return (
    <div className="space-y-2 rounded-md border p-3">
      <h2 className="text-lg font-medium">Pago mayor a lo reclamado</h2>
      <Field label="Valor pagado por la aseguradora">
        {(p) => (
          <Input
            {...p}
            inputMode="decimal"
            value={amount}
            onChange={(e) => {
              setAmount(e.target.value);
            }}
          />
        )}
      </Field>
      <Field label="Motivo">
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
      <Field label="Soporte">
        {(p) => (
          <Input
            {...p}
            maxLength={200}
            value={support}
            onChange={(e) => {
              setSupport(e.target.value);
            }}
          />
        )}
      </Field>
      <ErrorMessage message={problemMessage(pay.error)} />
      <Button
        disabled={!amount || reason.trim().length < 3 || support.trim().length < 3 || pay.isPending}
        onClick={() => {
          pay.mutate();
        }}
      >
        Aprobar y registrar
      </Button>
    </div>
  );
}

function Tracking({ claim, canManage }: { claim: Claim; canManage: boolean }) {
  const refresh = useRefresh();
  const [label, setLabel] = useState("");
  const [note, setNote] = useState("");
  const query = useQuery({
    queryKey: ["claims", "tracking", claim.id],
    queryFn: async () => {
      const res = await getClaimTracking(claim.id);
      if (res.status !== 200) throw new Error("seguimiento no disponible");
      return res.data;
    },
  });
  const act = useMutation({
    mutationFn: async (call: () => Promise<{ status: number }>) => {
      const res = await call();
      if (res.status !== 204) throw new Error("no guardado");
    },
    onSuccess: refresh,
  });
  return (
    <div className="space-y-3 rounded-md border p-3">
      <h2 className="text-lg font-medium">Soportes</h2>
      <ErrorMessage message={problemMessage(act.error)} />
      <ul className="space-y-1 text-sm">
        {query.data?.checklist.map((i) => (
          <li key={i.id}>
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={i.received}
                disabled={!canManage}
                onChange={(e) => {
                  const received = e.target.checked;
                  act.mutate(() => setChecklistItem(claim.id, i.id, { received }));
                }}
              />
              {i.label}
            </label>
          </li>
        ))}
      </ul>
      {canManage && (
        <div className="flex max-w-lg flex-wrap items-end gap-2">
          <Field label="Soporte esperado">
            {(p) => (
              <Input
                {...p}
                maxLength={200}
                value={label}
                onChange={(e) => {
                  setLabel(e.target.value);
                }}
              />
            )}
          </Field>
          <Button
            variant="outline"
            disabled={label.trim().length < 2}
            onClick={() => {
              act.mutate(() => addChecklistItem(claim.id, { label }));
              setLabel("");
            }}
          >
            Agregar
          </Button>
        </div>
      )}
      <h2 className="text-lg font-medium">Gestiones</h2>
      <ul className="space-y-1 text-sm">
        {query.data?.notes.map((n) => (
          <li key={n.id}>{n.body}</li>
        ))}
      </ul>
      {canManage && (
        <div className="space-y-2">
          <Field label="Nueva gestión" hint="No incluya datos de salud.">
            {(p) => (
              <Textarea
                {...p}
                maxLength={2000}
                value={note}
                onChange={(e) => {
                  setNote(e.target.value);
                }}
              />
            )}
          </Field>
          <Button
            variant="outline"
            disabled={note.trim().length < 3}
            onClick={() => {
              act.mutate(() => addClaimNote(claim.id, { body: note }));
              setNote("");
            }}
          >
            Guardar gestión
          </Button>
        </div>
      )}
    </div>
  );
}

/** Solo se descifra cuando el usuario lo pide: cada lectura queda en la bitácora. */
function Health({
  claim,
  canRead,
  canWrite,
}: {
  claim: Claim;
  canRead: boolean;
  canWrite: boolean;
}) {
  const refresh = useRefresh();
  const [draft, setDraft] = useState("");
  const [shown, setShown] = useState<string | null>(null);
  const reveal = useMutation({
    mutationFn: async () => {
      const res = await getClaimHealth(claim.id);
      if (res.status !== 200) throw new Error("detalle no disponible");
      setShown(res.data.details);
    },
  });
  const save = useMutation({
    mutationFn: async () => {
      const res = await setClaimHealth(claim.id, { details: draft });
      if (res.status !== 204) throw new Error("detalle no guardado");
      setDraft("");
      setShown(null);
    },
    onSuccess: refresh,
  });
  return (
    <div className="space-y-2 rounded-md border p-3">
      <h2 className="text-lg font-medium">Detalle de salud</h2>
      <p className="text-sm text-muted-foreground">
        Dato protegido: cada lectura queda registrada.
      </p>
      {canRead && claim.hasHealthDetails && shown === null && (
        <Button
          variant="outline"
          onClick={() => {
            reveal.mutate();
          }}
        >
          Ver detalle
        </Button>
      )}
      {shown !== null && <p className="text-sm whitespace-pre-wrap">{shown}</p>}
      <ErrorMessage message={problemMessage(reveal.error ?? save.error)} />
      {canWrite && (
        <>
          <Field label="Registrar detalle" hint="Reemplaza el anterior.">
            {(p) => (
              <Textarea
                {...p}
                autoComplete="off"
                maxLength={4000}
                value={draft}
                onChange={(e) => {
                  setDraft(e.target.value);
                }}
              />
            )}
          </Field>
          <Button
            variant="outline"
            disabled={draft.trim().length < 3 || save.isPending}
            onClick={() => {
              save.mutate();
            }}
          >
            Guardar detalle
          </Button>
        </>
      )}
    </div>
  );
}

/** Detalle del siniestro. Un asesor solo ve los datos del encabezado (DECISIONS N29). */
export function ClaimDetailPage({ claimId }: { claimId: string }) {
  const canManage = usePermission(permissions.claimManage);
  const canReadAll = usePermission(permissions.claimRead);
  const canOverride = usePermission(permissions.claimOverridePayment);
  const canHealthRead = usePermission(permissions.claimHealthRead);
  const canHealthWrite = usePermission(permissions.claimHealthWrite);
  const query = useQuery({
    queryKey: ["claims", "claim", claimId],
    queryFn: async () => {
      const res = await getClaim(claimId);
      if (res.status !== 200) throw new Error("siniestro no disponible");
      return res.data;
    },
  });
  const c = query.data;
  if (!c) {
    return (
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={false}
        emptyMessage=""
      />
    );
  }
  return (
    <section aria-labelledby="claim-title" className="space-y-4">
      <h1 id="claim-title" className="text-2xl font-semibold">
        {c.holderName} · {c.claimType}
      </h1>
      <p>Estado: {CLAIM_LABELS[c.status]}</p>
      {c.alertOutOfCoverage && (
        <p role="alert">Alerta: la fecha de ocurrencia está fuera de la vigencia.</p>
      )}
      {c.alertInArrears && <p role="alert">Alerta: la póliza estaba en mora al reportar.</p>}
      {c.alertNotInForce && <p role="alert">Alerta: la póliza no estaba en vigor al reportar.</p>}
      <dl className="grid gap-1 text-sm sm:grid-cols-2">
        <div>Póliza: {c.policyNumber ?? "Sin número"}</div>
        <div>
          Ocurrió: {formatDate(c.occurredOn)} en {c.location}
        </div>
        {c.insurerClaimNumber && <div>Número de la aseguradora: {c.insurerClaimNumber}</div>}
        {c.claimedAmount && <div>Reclamado: {formatMoney(c.claimedAmount, c.currency)}</div>}
        {c.paidAmount && <div>Pagado: {formatMoney(c.paidAmount, c.currency)}</div>}
        {c.decisionSupport && <div>Soporte: {c.decisionSupport}</div>}
      </dl>
      <p className="text-sm">{c.description}</p>
      {canManage && <Workflow claim={c} />}
      {canOverride && c.status === "approved" && <Override claim={c} />}
      {canReadAll && <Tracking claim={c} canManage={canManage} />}
      {(canHealthRead || canHealthWrite) && (
        <Health claim={c} canRead={canHealthRead} canWrite={canHealthWrite} />
      )}
    </section>
  );
}
