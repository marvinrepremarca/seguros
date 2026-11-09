import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState, type SubmitEvent } from "react";

import { formatDate, formatMoney } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState, Select } from "@/shared/ui/field";

import {
  acceptQuoteOffer,
  addQuoteOffer,
  declineQuoteRequest,
  getQuoteRequest,
  PaymentForm,
  type QuoteOffer,
  type QuoteRequest,
} from "../api/generated/quoting";
import type { QuotingDeps } from "../deps";
import { REQUEST_LABELS } from "../labels";

const PAYMENT_LABELS: Readonly<Record<PaymentForm, string>> = {
  single_payment: "Contado",
  monthly_installments: "Cuotas mensuales",
  quarterly_installments: "Cuotas trimestrales",
  automatic_debit: "Débito automático",
  premium_financing: "Financiación de primas",
};

function text(f: FormData, name: string): string {
  const v = f.get(name);
  return typeof v === "string" ? v.trim() : "";
}

function AddOffer({ request, deps }: { request: QuoteRequest; deps: QuotingDeps }) {
  const client = useQueryClient();
  const insurers = useQuery(deps.insurers);
  const add = useMutation({
    mutationFn: async (f: FormData) => {
      const validUntil = text(f, "validUntil");
      const reference = text(f, "reference");
      const deductible = text(f, "deductible");
      const res = await addQuoteOffer(request.id, {
        insurerId: text(f, "insurerId"),
        currency: text(f, "currency"),
        netPremium: text(f, "netPremium"),
        taxes: text(f, "taxes"),
        effectiveFrom: text(f, "effectiveFrom"),
        effectiveTo: text(f, "effectiveTo"),
        coverages: [
          {
            name: text(f, "coverage"),
            sumInsured: text(f, "sumInsured"),
            ...(deductible ? { deductible } : {}),
          },
        ],
        ...(validUntil ? { validUntil } : {}),
        ...(reference ? { reference } : {}),
      });
      if (res.status !== 200) throw new Error("oferta no registrada");
    },
    onSuccess: () => client.invalidateQueries({ queryKey: ["quoting"] }),
  });
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    add.mutate(new FormData(form), {
      onSuccess: () => {
        form.reset();
      },
    });
  };
  return (
    <form onSubmit={submit} className="grid gap-3 rounded-md border p-3 sm:grid-cols-2">
      <h2 className="text-lg font-medium sm:col-span-2">Registrar oferta</h2>
      <Field label="Aseguradora">
        {(p) => (
          <Select {...p} name="insurerId" required>
            <option value="">Seleccione</option>
            {insurers.data
              ?.filter((i) => i.active)
              .map((i) => (
                <option key={i.id} value={i.id}>
                  {i.name}
                </option>
              ))}
          </Select>
        )}
      </Field>
      <Field label="Moneda">
        {(p) => <Input {...p} name="currency" required defaultValue="COP" maxLength={3} />}
      </Field>
      <Field label="Prima neta">
        {(p) => <Input {...p} name="netPremium" required inputMode="decimal" />}
      </Field>
      <Field label="Impuestos y gastos">
        {(p) => <Input {...p} name="taxes" required inputMode="decimal" />}
      </Field>
      <Field label="Vigencia desde">
        {(p) => <Input {...p} name="effectiveFrom" type="date" required />}
      </Field>
      <Field label="Vigencia hasta">
        {(p) => <Input {...p} name="effectiveTo" type="date" required />}
      </Field>
      <Field label="Oferta válida hasta" hint="Vacío: los días configurados por la agencia.">
        {(p) => <Input {...p} name="validUntil" type="date" />}
      </Field>
      <Field label="Referencia de la aseguradora">
        {(p) => <Input {...p} name="reference" maxLength={60} />}
      </Field>
      <Field label="Cobertura">
        {(p) => <Input {...p} name="coverage" required maxLength={100} />}
      </Field>
      <Field label="Suma asegurada">
        {(p) => <Input {...p} name="sumInsured" required inputMode="decimal" />}
      </Field>
      <Field label="Deducible">{(p) => <Input {...p} name="deductible" maxLength={100} />}</Field>
      <div className="sm:col-span-2">
        <ErrorMessage message={problemMessage(add.error)} />
        <Button type="submit" disabled={add.isPending}>
          Guardar oferta
        </Button>
      </div>
    </form>
  );
}

function Accept({ request, offer }: { request: QuoteRequest; offer: QuoteOffer }) {
  const client = useQueryClient();
  const [form, setForm] = useState<PaymentForm>(PaymentForm.single_payment);
  const accept = useMutation({
    mutationFn: async () => {
      const res = await acceptQuoteOffer(request.id, { offerId: offer.id, paymentForm: form });
      if (res.status !== 200) throw new Error("oferta no aceptada");
    },
    onSuccess: () => client.invalidateQueries({ queryKey: ["quoting"] }),
  });
  return (
    <div className="space-y-2">
      <Field label="Forma de pago">
        {(p) => (
          <Select
            {...p}
            value={form}
            onChange={(e) => {
              const next = Object.values(PaymentForm).find((x) => x === e.target.value);
              if (next) setForm(next);
            }}
          >
            {Object.values(PaymentForm).map((x) => (
              <option key={x} value={x}>
                {PAYMENT_LABELS[x]}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <ErrorMessage message={problemMessage(accept.error)} />
      <Button
        disabled={accept.isPending}
        onClick={() => {
          accept.mutate();
        }}
      >
        Aceptar oferta
      </Button>
    </div>
  );
}

function Decline({ request }: { request: QuoteRequest }) {
  const client = useQueryClient();
  const [reason, setReason] = useState("");
  const decline = useMutation({
    mutationFn: async () => {
      const res = await declineQuoteRequest(request.id, { reason });
      if (res.status !== 200) throw new Error("no cerrada");
    },
    onSuccess: () => client.invalidateQueries({ queryKey: ["quoting"] }),
  });
  return (
    <div className="flex max-w-lg flex-wrap items-end gap-2">
      <Field label="Motivo de cierre">
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
        disabled={reason.trim().length < 3 || decline.isPending}
        onClick={() => {
          decline.mutate();
        }}
      >
        Cerrar sin aceptar
      </Button>
      <ErrorMessage message={problemMessage(decline.error)} />
    </div>
  );
}

/** Solicitud con el comparativo de ofertas. */
export function QuoteDetailPage({ requestId, deps }: { requestId: string; deps: QuotingDeps }) {
  const canManage = usePermission(permissions.quoteManageOwn);
  const insurers = useQuery(deps.insurers);
  const query = useQuery({
    queryKey: ["quoting", "request", requestId],
    queryFn: async () => {
      const res = await getQuoteRequest(requestId);
      if (res.status !== 200) throw new Error("solicitud no disponible");
      return res.data;
    },
  });
  const r = query.data;
  if (!r) {
    return (
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={false}
        emptyMessage=""
      />
    );
  }
  const open = r.status === "open" && canManage;
  return (
    <section aria-labelledby="quote-title" className="space-y-4">
      <h1 id="quote-title" className="text-2xl font-semibold">
        {r.holderName} · {r.riskDescription}
      </h1>
      <p>Estado: {REQUEST_LABELS[r.status]}</p>
      {r.declineReason && <p>Motivo: {r.declineReason}</p>}
      {r.policyId && (
        <Link to="/polizas/$policyId" params={{ policyId: r.policyId }} className="underline">
          Ver póliza en trámite
        </Link>
      )}
      <h2 className="text-lg font-medium">Comparativo</h2>
      <QueryState
        isPending={false}
        isError={false}
        isEmpty={r.offers.length === 0}
        emptyMessage="Aún no hay ofertas."
      />
      <div className="grid gap-3 md:grid-cols-2">
        {r.offers.map((o) => (
          <article key={o.id} className="space-y-1 rounded-md border p-3 text-sm">
            <h3 className="font-medium">
              {insurers.data?.find((i) => i.id === o.insurerId)?.name ?? "Aseguradora"}
            </h3>
            <p>Prima neta: {formatMoney(o.netPremium, o.currency)}</p>
            <p>Impuestos y gastos: {formatMoney(o.taxes, o.currency)}</p>
            <p>
              Vigencia: {formatDate(o.effectiveFrom)} a {formatDate(o.effectiveTo)}
            </p>
            <p>
              Válida hasta {formatDate(o.validUntil)}
              {o.expired && " (vencida)"}
            </p>
            <ul className="list-inside list-disc">
              {o.coverages.map((c) => (
                <li key={c.name}>
                  {c.name}: {formatMoney(c.sumInsured, o.currency)}
                  {c.deductible && ` · deducible ${c.deductible}`}
                </li>
              ))}
            </ul>
            {o.status === "accepted" && <p className="font-medium">Aceptada</p>}
            {open && !o.expired && <Accept request={r} offer={o} />}
          </article>
        ))}
      </div>
      {open && <AddOffer request={r} deps={deps} />}
      {open && <Decline request={r} />}
    </section>
  );
}
