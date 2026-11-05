import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { formatDate, formatDateTime, formatMoney } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState, Select, Textarea } from "@/shared/ui/field";

import {
  addCollectionActivity,
  adjustSchedule,
  CollectionChannel,
  getPlan,
  listCollectionActivities,
  listPayments,
  PaymentMethod,
  registerPayment,
  ReportedBy,
  reversePayment,
  type Plan,
} from "../api/generated/billing";
import { CHANNEL_LABELS, METHOD_LABELS, REPORTED_LABELS, STATUS_LABELS } from "../labels";

function useRefresh(policyId: string) {
  const client = useQueryClient();
  return () => client.invalidateQueries({ queryKey: ["billing", policyId] });
}

function parseOption<T extends string>(options: readonly T[], value: string, fallback: T): T {
  return options.find((o) => o === value) ?? fallback;
}

function PaymentForm({ policyId }: { policyId: string }) {
  const refresh = useRefresh(policyId);
  // La llave se conserva entre reintentos del mismo pago y cambia al registrarlo.
  const [key, setKey] = useState(() => crypto.randomUUID());
  const [amount, setAmount] = useState("");
  const [paidOn, setPaidOn] = useState("");
  const [reference, setReference] = useState("");
  const [method, setMethod] = useState<PaymentMethod>(PaymentMethod.transfer);
  const [reportedBy, setReportedBy] = useState<ReportedBy>(ReportedBy.client);
  const save = useMutation({
    mutationFn: async () => {
      const res = await registerPayment(
        policyId,
        { amount, paidOn, method, reportedBy, supportReference: reference },
        { headers: { "Idempotency-Key": key } },
      );
      if (res.status !== 201) throw new Error("pago no registrado");
    },
    onSuccess: async () => {
      setKey(crypto.randomUUID());
      setAmount("");
      setReference("");
      await refresh();
    },
  });
  return (
    <form
      className="grid max-w-2xl gap-3 sm:grid-cols-2"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <Field label="Valor pagado">
        {(p) => (
          <Input
            {...p}
            required
            inputMode="decimal"
            value={amount}
            onChange={(e) => {
              setAmount(e.target.value);
            }}
          />
        )}
      </Field>
      <Field label="Fecha de pago">
        {(p) => (
          <Input
            {...p}
            required
            type="date"
            value={paidOn}
            onChange={(e) => {
              setPaidOn(e.target.value);
            }}
          />
        )}
      </Field>
      <Field label="Medio de pago">
        {(p) => (
          <Select
            {...p}
            value={method}
            onChange={(e) => {
              setMethod(parseOption(Object.values(PaymentMethod), e.target.value, method));
            }}
          >
            {Object.values(PaymentMethod).map((m) => (
              <option key={m} value={m}>
                {METHOD_LABELS[m]}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <Field label="Informado por">
        {(p) => (
          <Select
            {...p}
            value={reportedBy}
            onChange={(e) => {
              setReportedBy(parseOption(Object.values(ReportedBy), e.target.value, reportedBy));
            }}
          >
            {Object.values(ReportedBy).map((r) => (
              <option key={r} value={r}>
                {REPORTED_LABELS[r]}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <Field label="Referencia del soporte">
        {(p) => (
          <Input
            {...p}
            required
            maxLength={200}
            value={reference}
            onChange={(e) => {
              setReference(e.target.value);
            }}
          />
        )}
      </Field>
      <div className="sm:col-span-2">
        <ErrorMessage message={problemMessage(save.error)} />
        <Button type="submit" disabled={save.isPending}>
          Registrar pago informado
        </Button>
      </div>
    </form>
  );
}

function Payments({ policyId }: { policyId: string }) {
  const canReverse = usePermission(permissions.billingPaymentReverse);
  const refresh = useRefresh(policyId);
  const [reason, setReason] = useState("");
  const query = useQuery({
    queryKey: ["billing", policyId, "payments"],
    queryFn: async () => {
      const res = await listPayments(policyId);
      if (res.status !== 200) throw new Error("pagos no disponibles");
      return res.data;
    },
  });
  const reverse = useMutation({
    mutationFn: async (paymentId: string) => {
      const res = await reversePayment(policyId, paymentId, { reason });
      if (res.status !== 204) throw new Error("reverso no aplicado");
    },
    onSuccess: async () => {
      setReason("");
      await refresh();
    },
  });
  return (
    <div className="space-y-3">
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={query.data?.length === 0}
        emptyMessage="Sin pagos registrados."
      />
      {canReverse && Boolean(query.data?.length) && (
        <div className="max-w-md">
          <Field label="Motivo del reverso" hint="Obligatorio para reversar un pago.">
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
        </div>
      )}
      <ErrorMessage message={problemMessage(reverse.error)} />
      <ul className="space-y-2 text-sm">
        {query.data?.map((p) => (
          <li key={p.id} className="flex flex-wrap items-center gap-3 rounded-md border p-3">
            <span>
              {formatDate(p.paidOn)} · {formatMoney(p.amount, p.currency)} ·{" "}
              {METHOD_LABELS[p.method]} · {p.supportReference}
              {p.reversedAt ? " · Reversado" : ""}
            </span>
            {canReverse && !p.reversedAt && (
              <Button
                variant="outline"
                disabled={reason.trim().length < 3 || reverse.isPending}
                onClick={() => {
                  reverse.mutate(p.id);
                }}
              >
                Reversar
              </Button>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

function Activities({ policyId }: { policyId: string }) {
  const canAdd = usePermission(permissions.billingCollectionManage);
  const refresh = useRefresh(policyId);
  const [note, setNote] = useState("");
  const [channel, setChannel] = useState<CollectionChannel>(CollectionChannel.call);
  const query = useQuery({
    queryKey: ["billing", policyId, "activities"],
    queryFn: async () => {
      const res = await listCollectionActivities(policyId);
      if (res.status !== 200) throw new Error("gestiones no disponibles");
      return res.data;
    },
  });
  const add = useMutation({
    mutationFn: async () => {
      const res = await addCollectionActivity(policyId, { channel, note });
      if (res.status !== 201) throw new Error("gestión no registrada");
    },
    onSuccess: async () => {
      setNote("");
      await refresh();
    },
  });
  return (
    <div className="space-y-3">
      {canAdd && (
        <form
          className="max-w-xl space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            add.mutate();
          }}
        >
          <Field label="Canal">
            {(p) => (
              <Select
                {...p}
                value={channel}
                onChange={(e) => {
                  setChannel(
                    parseOption(Object.values(CollectionChannel), e.target.value, channel),
                  );
                }}
              >
                {Object.values(CollectionChannel).map((c) => (
                  <option key={c} value={c}>
                    {CHANNEL_LABELS[c]}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="Resultado de la gestión">
            {(p) => (
              <Textarea
                {...p}
                required
                maxLength={1000}
                value={note}
                onChange={(e) => {
                  setNote(e.target.value);
                }}
              />
            )}
          </Field>
          <ErrorMessage message={problemMessage(add.error)} />
          <Button type="submit" variant="outline" disabled={add.isPending}>
            Registrar gestión
          </Button>
        </form>
      )}
      <ul className="space-y-2 text-sm">
        {query.data?.map((a) => (
          <li key={a.id} className="rounded-md border p-3">
            {formatDateTime(a.createdAt)} · {CHANNEL_LABELS[a.channel]}: {a.note}
          </li>
        ))}
      </ul>
    </div>
  );
}

interface Row {
  dueDate: string;
  amount: string;
}

/** Calendario que informó la aseguradora (N15): reemplaza las cuotas sin abonos; la suma no cambia. */
function ScheduleForm({ plan }: { plan: Plan }) {
  const refresh = useRefresh(plan.policyId);
  const [rows, setRows] = useState<Row[]>(() =>
    plan.installments
      .filter((i) => i.status === "pending" && /^0+(\.0+)?$/.test(i.paid))
      .map((i) => ({ dueDate: i.dueDate, amount: i.amount })),
  );
  const [support, setSupport] = useState("");
  const save = useMutation({
    mutationFn: async () => {
      const res = await adjustSchedule(plan.policyId, {
        version: plan.version,
        supportReference: support,
        installments: rows,
      });
      if (res.status !== 200) throw new Error("plan no ajustado");
    },
    onSuccess: refresh,
  });
  const update = (index: number, change: Partial<Row>) => {
    setRows(rows.map((r, i) => (i === index ? { ...r, ...change } : r)));
  };
  return (
    <form
      className="max-w-xl space-y-3"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      {rows.map((r, i) => (
        <div key={i} className="grid gap-3 sm:grid-cols-2">
          <Field label={`Vencimiento ${String(i + 1)}`}>
            {(p) => (
              <Input
                {...p}
                required
                type="date"
                value={r.dueDate}
                onChange={(e) => {
                  update(i, { dueDate: e.target.value });
                }}
              />
            )}
          </Field>
          <Field label={`Valor ${String(i + 1)}`}>
            {(p) => (
              <Input
                {...p}
                required
                inputMode="decimal"
                value={r.amount}
                onChange={(e) => {
                  update(i, { amount: e.target.value });
                }}
              />
            )}
          </Field>
        </div>
      ))}
      <Field label="Referencia del soporte de la aseguradora">
        {(p) => (
          <Input
            {...p}
            required
            maxLength={200}
            value={support}
            onChange={(e) => {
              setSupport(e.target.value);
            }}
          />
        )}
      </Field>
      <ErrorMessage message={problemMessage(save.error)} />
      <div className="flex gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            setRows([...rows, { dueDate: "", amount: "" }]);
          }}
        >
          Agregar cuota
        </Button>
        <Button type="submit" disabled={save.isPending}>
          Guardar calendario
        </Button>
      </div>
    </form>
  );
}

export function PlanPage({ policyId }: { policyId: string }) {
  const canPay = usePermission(permissions.billingPaymentRegister);
  const canAdjust = usePermission(permissions.billingPlanAdjust);
  const query = useQuery({
    queryKey: ["billing", policyId, "plan"],
    queryFn: async () => {
      const res = await getPlan(policyId);
      if (res.status !== 200) throw new Error("plan no disponible");
      return res.data;
    },
  });
  const plan = query.data;
  if (!plan) {
    return (
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={false}
        emptyMessage=""
      />
    );
  }
  const money = (v: string) => formatMoney(v, plan.currency);
  return (
    <section aria-labelledby="plan-title" className="space-y-6">
      <h1 id="plan-title" className="text-2xl font-semibold">
        Plan de pagos · Póliza {plan.policyNumber}
      </h1>
      <p>
        Total {money(plan.total)} · Saldo a favor {money(plan.credit)}
        {plan.inArrears ? " · En mora" : ""}
        {plan.cancelled ? " · Póliza cancelada" : ""}
      </p>
      {plan.refundAmount && plan.refundDate && (
        <p>
          Devolución informada: {money(plan.refundAmount)} desde {formatDate(plan.refundDate)}. La
          paga la aseguradora al cliente.
        </p>
      )}
      <div className="overflow-x-auto">
        <table className="w-full text-left text-sm">
          <caption className="sr-only">Cuotas</caption>
          <thead>
            <tr className="border-b">
              <th className="p-2">Cuota</th>
              <th className="p-2">Vence</th>
              <th className="p-2">Valor</th>
              <th className="p-2">Abonado</th>
              <th className="p-2">Estado</th>
            </tr>
          </thead>
          <tbody>
            {plan.installments.map((i) => (
              <tr key={i.id} className="border-b">
                <td className="p-2">{i.number}</td>
                <td className="p-2">{formatDate(i.dueDate)}</td>
                <td className="p-2">{money(i.amount)}</td>
                <td className="p-2">{money(i.paid)}</td>
                <td className="p-2">{STATUS_LABELS[i.status]}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {canPay && !plan.cancelled && (
        <div className="space-y-2">
          <h2 className="text-lg font-medium">Registrar pago</h2>
          <PaymentForm policyId={policyId} />
        </div>
      )}
      <div className="space-y-2">
        <h2 className="text-lg font-medium">Pagos</h2>
        <Payments policyId={policyId} />
      </div>
      <div className="space-y-2">
        <h2 className="text-lg font-medium">Gestión de cobro</h2>
        <Activities policyId={policyId} />
      </div>
      {canAdjust && !plan.cancelled && (
        <div className="space-y-2">
          <h2 className="text-lg font-medium">Ajustar calendario</h2>
          <ScheduleForm key={plan.version} plan={plan} />
        </div>
      )}
    </section>
  );
}
