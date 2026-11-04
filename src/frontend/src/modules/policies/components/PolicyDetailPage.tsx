import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState, type SyntheticEvent, type ReactNode } from "react";

import { formatDate, formatDateTime, formatMoney } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { requiredReason, type TransferReasonCode } from "@/shared/lib/transferReasons";
import { TransferReasonFields } from "@/shared/ui/TransferReasonFields";
import { ErrorMessage, Field, Input, QueryState, Select, Textarea } from "@/shared/ui/field";

import { availableActions } from "../actions";
import {
  acceptPolicy,
  CancellationCause,
  cancelPolicy,
  EndorsementType,
  getPolicy,
  issueEndorsement,
  issuePolicy,
  PartyRole,
  rejectEndorsement,
  reassignPolicyAgent,
  rejectPolicy,
  requestEndorsement,
  type Endorsement,
  type Policy,
} from "../api/generated/policies";
import type { PolicyDeps } from "../deps";
import {
  CAUSE_LABELS,
  ENDORSEMENT_LABELS,
  ENDORSEMENT_STATUS_LABELS,
  PAYMENT_LABELS,
  ROLE_LABELS,
  STATUS_LABELS,
} from "../labels";
import { agentsQuery } from "../queries";
import { fromRiskData, riskFields, toRiskData } from "../risk";
import { amount, signedAmount } from "../schemas";
import { RiskFields } from "./RiskFields";

const KEY = "policies";

function useApply(policy: Policy) {
  const queryClient = useQueryClient();
  return (action: () => Promise<unknown>, onDone?: () => void) => ({
    mutationFn: action,
    onSuccess: async () => {
      onDone?.();
      await queryClient.invalidateQueries({ queryKey: [KEY] });
      await queryClient.invalidateQueries({ queryKey: [KEY, "detail", policy.id] });
    },
  });
}

function submit(fn: () => void) {
  return (e: SyntheticEvent) => {
    e.preventDefault();
    fn();
  };
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <h2 className="text-lg font-semibold">{title}</h2>
      {children}
    </section>
  );
}

export function PolicyDetailPage({ policyId, deps }: { policyId: string; deps: PolicyDeps }) {
  const query = useQuery({
    queryKey: [KEY, "detail", policyId],
    queryFn: async () => {
      const res = await getPolicy(policyId);
      if (res.status !== 200) throw new Error("póliza no disponible");
      return res.data;
    },
  });
  if (query.data === undefined) {
    return (
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={false}
        emptyMessage=""
      />
    );
  }
  return <Detail policy={query.data} deps={deps} />;
}

function Detail({ policy, deps }: { policy: Policy; deps: PolicyDeps }) {
  const insurers = useQuery(deps.insurers);
  const lines = useQuery(deps.lines);
  const money = (v: string) => formatMoney(v, policy.currency);
  return (
    <div className="max-w-3xl space-y-8">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">
          {policy.policyNumber ? `Póliza ${policy.policyNumber}` : "Póliza sin número"}
        </h1>
        <p>
          <strong>{STATUS_LABELS[policy.status]}</strong>
          {" · "}
          {insurers.data?.find((i) => i.id === policy.insurerId)?.name}
          {" · "}
          {lines.data?.find((l) => l.id === policy.lineOfBusinessId)?.name}
        </p>
      </header>

      <Actions policy={policy} deps={deps} />

      <Section title="Prima y vigencia">
        <dl className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-2">
          <div>
            <dt className="inline text-muted-foreground">Prima neta: </dt>
            <dd className="inline">{money(policy.netPremium)}</dd>
          </div>
          <div>
            <dt className="inline text-muted-foreground">Impuestos y gastos: </dt>
            <dd className="inline">{money(policy.taxes)}</dd>
          </div>
          <div>
            <dt className="inline text-muted-foreground">Total: </dt>
            <dd className="inline">{money(policy.totalPremium)}</dd>
          </div>
          <div>
            <dt className="inline text-muted-foreground">Forma de pago: </dt>
            <dd className="inline">{PAYMENT_LABELS[policy.paymentForm]}</dd>
          </div>
          <div>
            <dt className="inline text-muted-foreground">Vigencia: </dt>
            <dd className="inline">
              {formatDate(policy.effectiveFrom)} a {formatDate(policy.effectiveTo)}
            </dd>
          </div>
          {policy.issuedAt && (
            <div>
              <dt className="inline text-muted-foreground">Expedida: </dt>
              <dd className="inline">{formatDateTime(policy.issuedAt)}</dd>
            </div>
          )}
        </dl>
        {policy.rejectionReason && (
          <p className="text-sm">Motivo del rechazo: {policy.rejectionReason}</p>
        )}
        {policy.cancellation && (
          <p className="text-sm">
            Cancelada desde el {formatDate(policy.cancellation.effectiveDate)} por{" "}
            {CAUSE_LABELS[policy.cancellation.cause].toLowerCase()}. Prima no devengada informada:{" "}
            {money(policy.cancellation.unearnedPremium)}.
          </p>
        )}
      </Section>

      <Section title="Partes">
        <ul className="text-sm">
          {policy.parties.map((p) => (
            <li key={`${p.partyId}-${p.role}`}>
              {ROLE_LABELS[p.role]}: {p.name}
            </li>
          ))}
        </ul>
      </Section>

      <Section title="Coberturas">
        {policy.coverages.length === 0 && (
          <p className="text-sm text-muted-foreground">Sin coberturas.</p>
        )}
        <ul className="text-sm">
          {policy.coverages.map((c) => (
            <li key={c.id}>
              {c.name}: {money(c.sumInsured)}
              {c.deductible && `, deducible ${c.deductible}`}
            </li>
          ))}
        </ul>
      </Section>

      <Section title="Bienes o personas aseguradas">
        {policy.items.length === 0 && <p className="text-sm text-muted-foreground">Sin bienes.</p>}
        <ul className="space-y-2 text-sm">
          {policy.items.map((i) => (
            <li key={i.id}>
              <strong>{i.description}</strong>
              <RiskSummary schemaKey={i.riskSchemaKey} data={i.riskData} deps={deps} />
            </li>
          ))}
        </ul>
      </Section>

      <Endorsements policy={policy} deps={deps} />
    </div>
  );
}

function RiskSummary({
  schemaKey,
  data,
  deps,
}: {
  schemaKey: string;
  data: Readonly<Record<string, unknown>>;
  deps: PolicyDeps;
}) {
  const schemas = useQuery(deps.riskSchemas);
  const fields = riskFields(schemas.data?.find((s) => s.key === schemaKey)?.schema);
  const values = fromRiskData(data);
  return (
    <span className="text-muted-foreground">
      {fields.flatMap((f) => (values[f.name] ? [` · ${f.label}: ${values[f.name] ?? ""}`] : []))}
    </span>
  );
}

type OpenForm = "issue" | "reject" | "cancel" | "endorse" | "reassign" | null;

function Actions({ policy, deps }: { policy: Policy; deps: PolicyDeps }) {
  const actions = availableActions(policy.status, {
    manage: usePermission(permissions.policyManage),
    cancel: usePermission(permissions.policyCancel),
    endorse: usePermission(permissions.endorsementManage),
    assign: usePermission(permissions.policyAssign),
  });
  const [open, setOpen] = useState<OpenForm>(null);
  const apply = useApply(policy);
  const accept = useMutation(apply(() => acceptPolicy(policy.id, { version: policy.version })));
  if (actions.length === 0) return null;
  const toggle = (form: Exclude<OpenForm, null>) => () => {
    setOpen(open === form ? null : form);
  };
  const done = () => {
    setOpen(null);
  };
  return (
    <div className="space-y-4">
      <ErrorMessage message={problemMessage(accept.error)} />
      <div className="flex flex-wrap gap-2">
        {actions.includes("edit") && (
          <Button asChild variant="outline">
            <Link to="/polizas/$policyId/editar" params={{ policyId: policy.id }}>
              Ajustar datos
            </Link>
          </Button>
        )}
        {actions.includes("accept") && (
          <Button
            disabled={accept.isPending}
            onClick={() => {
              accept.mutate();
            }}
          >
            El cliente aceptó
          </Button>
        )}
        {actions.includes("issue") && (
          <Button onClick={toggle("issue")}>Registrar expedición</Button>
        )}
        {actions.includes("reject") && (
          <Button variant="outline" onClick={toggle("reject")}>
            Registrar rechazo
          </Button>
        )}
        {actions.includes("endorse") && (
          <Button variant="outline" onClick={toggle("endorse")}>
            Solicitar endoso
          </Button>
        )}
        {actions.includes("reassign") && (
          <Button variant="outline" onClick={toggle("reassign")}>
            Cambiar asesor
          </Button>
        )}
        {actions.includes("cancel") && (
          <Button variant="destructive" onClick={toggle("cancel")}>
            Registrar cancelación
          </Button>
        )}
      </div>
      {open === "issue" && <IssueForm policy={policy} onDone={done} />}
      {open === "reject" && <RejectForm policy={policy} onDone={done} />}
      {open === "cancel" && <CancelForm policy={policy} onDone={done} />}
      {open === "reassign" && <ReassignForm policy={policy} onDone={done} />}
      {open === "endorse" && <RequestEndorsementForm policy={policy} onDone={done} deps={deps} />}
    </div>
  );
}

interface FormProps {
  policy: Policy;
  onDone: () => void;
}

function IssueForm({ policy, onDone }: FormProps) {
  const [number, setNumber] = useState("");
  const apply = useApply(policy);
  const issue = useMutation(
    apply(
      () => issuePolicy(policy.id, { policyNumber: number }, { version: policy.version }),
      onDone,
    ),
  );
  return (
    <form
      className="space-y-3 rounded border p-4"
      onSubmit={submit(() => {
        issue.mutate();
      })}
    >
      <ErrorMessage message={problemMessage(issue.error)} />
      <Field label="Número de póliza" hint="El que asignó la aseguradora">
        {(p) => (
          <Input
            {...p}
            required
            maxLength={60}
            value={number}
            onChange={(e) => {
              setNumber(e.target.value);
            }}
          />
        )}
      </Field>
      <Button type="submit" disabled={issue.isPending}>
        Expedir
      </Button>
    </form>
  );
}

function RejectForm({ policy, onDone }: FormProps) {
  const [reason, setReason] = useState("");
  const apply = useApply(policy);
  const reject = useMutation(
    apply(() => rejectPolicy(policy.id, { reason }, { version: policy.version }), onDone),
  );
  return (
    <form
      className="space-y-3 rounded border p-4"
      onSubmit={submit(() => {
        reject.mutate();
      })}
    >
      <ErrorMessage message={problemMessage(reject.error)} />
      <Field label="Motivo del rechazo">
        {(p) => (
          <Textarea
            {...p}
            required
            minLength={3}
            maxLength={500}
            value={reason}
            onChange={(e) => {
              setReason(e.target.value);
            }}
          />
        )}
      </Field>
      <Button type="submit" disabled={reject.isPending}>
        Confirmar rechazo
      </Button>
    </form>
  );
}

function ReassignForm({ policy, onDone }: FormProps) {
  const agents = useQuery(agentsQuery());
  const [agentId, setAgentId] = useState("");
  const [reason, setReason] = useState<TransferReasonCode | "">("");
  const [note, setNote] = useState("");
  const apply = useApply(policy);
  const reassign = useMutation(
    apply(
      () =>
        reassignPolicyAgent(
          policy.id,
          { agentId, reason: requiredReason(reason), ...(note ? { note } : {}) },
          { version: policy.version },
        ),
      onDone,
    ),
  );
  const current = agents.data?.find((a) => a.id === policy.agentId);
  return (
    <form
      className="space-y-3 rounded border p-4"
      onSubmit={submit(() => {
        reassign.mutate();
      })}
    >
      <p className="text-sm text-muted-foreground">
        Asesor actual: {current?.displayName ?? "sin asesor"}. El cambio queda en el historial de la
        póliza.
      </p>
      <ErrorMessage message={problemMessage(reassign.error)} />
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Asesor nuevo">
          {(p) => (
            <Select
              {...p}
              required
              value={agentId}
              onChange={(e) => {
                setAgentId(e.target.value);
              }}
            >
              <option value="">Selecciona…</option>
              {agents.data
                ?.filter((a) => a.active && a.id !== policy.agentId)
                .map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.displayName}
                  </option>
                ))}
            </Select>
          )}
        </Field>
        <TransferReasonFields
          reason={reason}
          note={note}
          onReasonChange={setReason}
          onNoteChange={setNote}
        />
      </div>
      <Button type="submit" disabled={reassign.isPending}>
        Confirmar cambio de asesor
      </Button>
    </form>
  );
}

function parseCause(value: string): CancellationCause {
  return Object.values(CancellationCause).find((c) => c === value) ?? CancellationCause.other;
}

function CancelForm({ policy, onDone }: FormProps) {
  const [cause, setCause] = useState<CancellationCause>(CancellationCause.client_request);
  const [effectiveDate, setEffectiveDate] = useState("");
  const [unearned, setUnearned] = useState("0");
  const [reference, setReference] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const apply = useApply(policy);
  const cancel = useMutation(
    apply(
      () =>
        cancelPolicy(
          policy.id,
          {
            cause,
            effectiveDate,
            unearnedPremium: unearned,
            ...(reference ? { insurerReference: reference } : {}),
          },
          { version: policy.version },
        ),
      onDone,
    ),
  );
  return (
    <form
      className="space-y-3 rounded border p-4"
      onSubmit={submit(() => {
        if (!amount.safeParse(unearned).success) {
          setLocalError(
            "La prima no devengada no es un monto válido: usa punto para los decimales.",
          );
          return;
        }
        setLocalError(null);
        cancel.mutate();
      })}
    >
      <p className="text-sm text-muted-foreground">
        Registra la cancelación que ya expidió la aseguradora. La póliza queda cancelada de forma
        definitiva.
      </p>
      <ErrorMessage message={localError ?? problemMessage(cancel.error)} />
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Causa">
          {(p) => (
            <Select
              {...p}
              value={cause}
              onChange={(e) => {
                setCause(parseCause(e.target.value));
              }}
            >
              {Object.values(CancellationCause).map((c) => (
                <option key={c} value={c}>
                  {CAUSE_LABELS[c]}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field label="Fecha efectiva">
          {(p) => (
            <Input
              {...p}
              required
              type="date"
              value={effectiveDate}
              onChange={(e) => {
                setEffectiveDate(e.target.value);
              }}
            />
          )}
        </Field>
        <Field label="Prima no devengada" hint="El valor que informó la aseguradora">
          {(p) => (
            <Input
              {...p}
              required
              inputMode="decimal"
              value={unearned}
              onChange={(e) => {
                setUnearned(e.target.value);
              }}
            />
          )}
        </Field>
        <Field label="Referencia de la aseguradora (opcional)">
          {(p) => (
            <Input
              {...p}
              maxLength={60}
              value={reference}
              onChange={(e) => {
                setReference(e.target.value);
              }}
            />
          )}
        </Field>
      </div>
      <Button type="submit" variant="destructive" disabled={cancel.isPending}>
        Confirmar cancelación
      </Button>
    </form>
  );
}

function parseEndorsementType(value: string): EndorsementType {
  return Object.values(EndorsementType).find((t) => t === value) ?? EndorsementType.other;
}

function RequestEndorsementForm({ policy, onDone }: FormProps & { deps: PolicyDeps }) {
  const [type, setType] = useState<EndorsementType>(EndorsementType.data_change);
  const [description, setDescription] = useState("");
  const [effectiveDate, setEffectiveDate] = useState("");
  const apply = useApply(policy);
  const request = useMutation(
    apply(
      () =>
        requestEndorsement(
          policy.id,
          { type, description, effectiveDate },
          { version: policy.version },
        ),
      onDone,
    ),
  );
  return (
    <form
      className="space-y-3 rounded border p-4"
      onSubmit={submit(() => {
        request.mutate();
      })}
    >
      <p className="text-sm text-muted-foreground">
        La póliza no cambia hasta que registres el endoso emitido por la aseguradora.
      </p>
      <ErrorMessage message={problemMessage(request.error)} />
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Tipo de endoso">
          {(p) => (
            <Select
              {...p}
              value={type}
              onChange={(e) => {
                setType(parseEndorsementType(e.target.value));
              }}
            >
              {Object.values(EndorsementType).map((t) => (
                <option key={t} value={t}>
                  {ENDORSEMENT_LABELS[t]}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field label="Fecha efectiva del endoso">
          {(p) => (
            <Input
              {...p}
              required
              type="date"
              value={effectiveDate}
              onChange={(e) => {
                setEffectiveDate(e.target.value);
              }}
            />
          )}
        </Field>
      </div>
      <Field label="Descripción de la solicitud">
        {(p) => (
          <Textarea
            {...p}
            required
            minLength={3}
            maxLength={500}
            value={description}
            onChange={(e) => {
              setDescription(e.target.value);
            }}
          />
        )}
      </Field>
      <Button type="submit" disabled={request.isPending}>
        Registrar solicitud
      </Button>
    </form>
  );
}

function Endorsements({ policy, deps }: { policy: Policy; deps: PolicyDeps }) {
  const canManage = usePermission(permissions.endorsementManage);
  if (policy.endorsements.length === 0) return null;
  return (
    <Section title="Endosos">
      <ul className="space-y-3 text-sm">
        {policy.endorsements.map((e) => (
          <li key={e.id} className="space-y-2 rounded border p-3">
            <p>
              <strong>{ENDORSEMENT_LABELS[e.type]}</strong> · {ENDORSEMENT_STATUS_LABELS[e.status]}
              {e.number && ` · N.º ${e.number}`} · efectivo el {formatDate(e.effectiveDate)}
            </p>
            <p>{e.description}</p>
            {e.netPremiumDelta !== undefined && (
              <p className="text-muted-foreground">
                Variación de prima neta: {formatMoney(e.netPremiumDelta, policy.currency)}; de
                impuestos: {formatMoney(e.taxesDelta ?? "0", policy.currency)}.
              </p>
            )}
            {e.rejectionReason && (
              <p className="text-muted-foreground">Motivo: {e.rejectionReason}</p>
            )}
            {canManage && e.status === "requested" && (
              <ResolveEndorsement policy={policy} endorsement={e} deps={deps} />
            )}
          </li>
        ))}
      </ul>
    </Section>
  );
}

function parseAddRole(value: string): PartyRole {
  return value === PartyRole.insured ? PartyRole.insured : PartyRole.beneficiary;
}

function ResolveEndorsement({
  policy,
  endorsement,
  deps,
}: {
  policy: Policy;
  endorsement: Endorsement;
  deps: PolicyDeps;
}) {
  const [mode, setMode] = useState<"issue" | "reject" | null>(null);
  const [number, setNumber] = useState("");
  const [netDelta, setNetDelta] = useState("0");
  const [taxesDelta, setTaxesDelta] = useState("0");
  const [sums, setSums] = useState<Record<string, string>>({});
  const [risks, setRisks] = useState<Record<string, Record<string, string>>>({});
  const [addParty, setAddParty] = useState("");
  const [addRole, setAddRole] = useState<PartyRole>(PartyRole.beneficiary);
  const [remove, setRemove] = useState<Record<string, boolean>>({});
  const [reason, setReason] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const schemas = useQuery(deps.riskSchemas);
  const apply = useApply(policy);
  const done = () => {
    setMode(null);
  };
  const fieldsOf = (key: string) => riskFields(schemas.data?.find((s) => s.key === key)?.schema);

  const issue = useMutation(
    apply(
      () =>
        issueEndorsement(
          policy.id,
          endorsement.id,
          {
            number,
            netPremiumDelta: netDelta,
            taxesDelta,
            coverageChanges: Object.entries(sums)
              .filter(([, v]) => v.trim() !== "")
              .map(([coverageId, sumInsured]) => ({ coverageId, sumInsured })),
            itemChanges: policy.items.flatMap((i) => {
              const changed = risks[i.id];
              return changed
                ? [{ itemId: i.id, riskData: toRiskData(fieldsOf(i.riskSchemaKey), changed) }]
                : [];
            }),
            addParties: addParty ? [{ partyId: addParty, role: addRole }] : [],
            removeParties: policy.parties.flatMap((p) =>
              remove[`${p.partyId}-${p.role}`] ? [{ partyId: p.partyId, role: p.role }] : [],
            ),
          },
          { version: policy.version },
        ),
      done,
    ),
  );
  const reject = useMutation(
    apply(
      () => rejectEndorsement(policy.id, endorsement.id, { reason }, { version: policy.version }),
      done,
    ),
  );

  if (mode === null) {
    return (
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          onClick={() => {
            setMode("issue");
          }}
        >
          Registrar endoso emitido
        </Button>
        <Button
          size="sm"
          variant="outline"
          onClick={() => {
            setMode("reject");
          }}
        >
          Registrar rechazo del endoso
        </Button>
      </div>
    );
  }
  if (mode === "reject") {
    return (
      <form
        className="space-y-2"
        onSubmit={submit(() => {
          reject.mutate();
        })}
      >
        <ErrorMessage message={problemMessage(reject.error)} />
        <Field label="Motivo del rechazo del endoso">
          {(p) => (
            <Textarea
              {...p}
              required
              minLength={3}
              maxLength={500}
              value={reason}
              onChange={(e) => {
                setReason(e.target.value);
              }}
            />
          )}
        </Field>
        <Button type="submit" size="sm" disabled={reject.isPending}>
          Confirmar rechazo del endoso
        </Button>
      </form>
    );
  }
  return (
    <form
      className="space-y-4"
      onSubmit={submit(() => {
        if (
          !signedAmount.safeParse(netDelta).success ||
          !signedAmount.safeParse(taxesDelta).success
        ) {
          setLocalError("Las variaciones deben ser montos válidos: usa punto para los decimales.");
          return;
        }
        setLocalError(null);
        issue.mutate();
      })}
    >
      <ErrorMessage message={localError ?? problemMessage(issue.error)} />
      <div className="grid gap-4 sm:grid-cols-3">
        <Field label="Número del endoso">
          {(p) => (
            <Input
              {...p}
              required
              maxLength={60}
              value={number}
              onChange={(e) => {
                setNumber(e.target.value);
              }}
            />
          )}
        </Field>
        <Field label="Variación de prima neta" hint="Negativa si hay devolución">
          {(p) => (
            <Input
              {...p}
              required
              value={netDelta}
              onChange={(e) => {
                setNetDelta(e.target.value);
              }}
            />
          )}
        </Field>
        <Field label="Variación de impuestos">
          {(p) => (
            <Input
              {...p}
              required
              value={taxesDelta}
              onChange={(e) => {
                setTaxesDelta(e.target.value);
              }}
            />
          )}
        </Field>
      </div>

      {policy.coverages.length > 0 && (
        <fieldset className="space-y-2">
          <legend className="font-medium">Nueva suma asegurada (déjala vacía si no cambia)</legend>
          <div className="grid gap-4 sm:grid-cols-2">
            {policy.coverages.map((c) => (
              <Field
                key={c.id}
                label={`${c.name} (hoy ${formatMoney(c.sumInsured, policy.currency)})`}
              >
                {(p) => (
                  <Input
                    {...p}
                    inputMode="decimal"
                    value={sums[c.id] ?? ""}
                    onChange={(e) => {
                      setSums({ ...sums, [c.id]: e.target.value });
                    }}
                  />
                )}
              </Field>
            ))}
          </div>
        </fieldset>
      )}

      {policy.items.map((i) => (
        <fieldset key={i.id} className="space-y-2">
          <legend className="font-medium">Datos de {i.description}</legend>
          {risks[i.id] === undefined ? (
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => {
                setRisks({ ...risks, [i.id]: fromRiskData(i.riskData) });
              }}
            >
              Cambiar datos de {i.description}
            </Button>
          ) : (
            <RiskFields
              fields={fieldsOf(i.riskSchemaKey)}
              values={risks[i.id] ?? {}}
              onChange={(name, value) => {
                setRisks({ ...risks, [i.id]: { ...risks[i.id], [name]: value } });
              }}
            />
          )}
        </fieldset>
      ))}

      <fieldset className="space-y-2">
        <legend className="font-medium">Partes</legend>
        <ul className="space-y-1">
          {policy.parties
            .filter((p) => p.role !== "holder")
            .map((p) => {
              const key = `${p.partyId}-${p.role}`;
              return (
                <li key={key}>
                  <label className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      checked={remove[key] ?? false}
                      onChange={(e) => {
                        setRemove({ ...remove, [key]: e.target.checked });
                      }}
                    />
                    Excluir a {p.name} ({ROLE_LABELS[p.role].toLowerCase()})
                  </label>
                </li>
              );
            })}
        </ul>
        <deps.PartyPicker label="Cliente a incluir" value={addParty} onChange={setAddParty} />
        <div className="max-w-xs">
          <Field label="Papel del cliente a incluir">
            {(p) => (
              <Select
                {...p}
                value={addRole}
                onChange={(e) => {
                  setAddRole(parseAddRole(e.target.value));
                }}
              >
                <option value={PartyRole.beneficiary}>{ROLE_LABELS.beneficiary}</option>
                <option value={PartyRole.insured}>{ROLE_LABELS.insured}</option>
              </Select>
            )}
          </Field>
        </div>
      </fieldset>

      <Button type="submit" size="sm" disabled={issue.isPending}>
        Aplicar endoso
      </Button>
    </form>
  );
}
