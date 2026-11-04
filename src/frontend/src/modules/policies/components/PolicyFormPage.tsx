import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Controller, useFieldArray, useForm, useWatch } from "react-hook-form";

import { problemMessage } from "@/shared/lib/problem";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState, Select } from "@/shared/ui/field";

import {
  createPolicy,
  getPolicy,
  listAgents,
  updatePolicyTerms,
  type Policy,
  type PolicyTerms,
} from "../api/generated/policies";
import type { PolicyDeps } from "../deps";
import { PAYMENT_LABELS, ROLE_LABELS } from "../labels";
import { fromRiskData, riskFields, toRiskData } from "../risk";
import { policyFormSchema, type PolicyFormValues } from "../schemas";
import { RiskFields } from "./RiskFields";

const EMPTY: PolicyFormValues = {
  insurerId: "",
  lineOfBusinessId: "",
  productId: "",
  agentId: "",
  currency: "COP",
  netPremium: "",
  taxes: "0",
  effectiveFrom: "",
  effectiveTo: "",
  paymentForm: "single_payment",
  holderId: "",
  parties: [{ partyId: "", role: "insured" }],
  coverages: [{ name: "", sumInsured: "", deductible: "" }],
  items: [{ description: "", risk: {} }],
};

function fromPolicy(p: Policy): PolicyFormValues {
  return {
    insurerId: p.insurerId,
    lineOfBusinessId: p.lineOfBusinessId,
    productId: p.productId ?? "",
    agentId: p.agentId ?? "",
    currency: p.currency,
    netPremium: p.netPremium,
    taxes: p.taxes,
    effectiveFrom: p.effectiveFrom,
    effectiveTo: p.effectiveTo,
    paymentForm: p.paymentForm,
    holderId: p.parties.find((x) => x.role === "holder")?.partyId ?? "",
    parties: p.parties.flatMap((x) =>
      x.role === "holder" ? [] : [{ partyId: x.partyId, role: x.role }],
    ),
    coverages: p.coverages.map((c) => ({
      name: c.name,
      sumInsured: c.sumInsured,
      deductible: c.deductible ?? "",
    })),
    items: p.items.map((i) => ({ description: i.description, risk: fromRiskData(i.riskData) })),
  };
}

export function PolicyFormPage({ deps, policyId }: { deps: PolicyDeps; policyId?: string }) {
  const existing = useQuery({
    queryKey: ["policies", "detail", policyId],
    enabled: policyId !== undefined,
    queryFn: async () => {
      const res = await getPolicy(policyId ?? "");
      if (res.status !== 200) throw new Error("póliza no disponible");
      return res.data;
    },
  });
  if (policyId !== undefined && existing.data === undefined) {
    return (
      <QueryState
        isPending={existing.isPending}
        isError={existing.isError}
        isEmpty={false}
        emptyMessage=""
      />
    );
  }
  return <PolicyForm deps={deps} {...(existing.data ? { policy: existing.data } : {})} />;
}

function PolicyForm({ deps, policy }: { deps: PolicyDeps; policy?: Policy }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const insurers = useQuery(deps.insurers);
  const lines = useQuery(deps.lines);
  const products = useQuery(deps.products);
  const schemas = useQuery(deps.riskSchemas);
  const agents = useQuery({
    queryKey: ["policies", "agents"],
    queryFn: async () => {
      const res = await listAgents();
      if (res.status !== 200) throw new Error("asesores no disponibles");
      return res.data.items;
    },
  });
  const form = useForm({
    resolver: zodResolver(policyFormSchema),
    defaultValues: policy ? fromPolicy(policy) : EMPTY,
  });
  const parties = useFieldArray({ control: form.control, name: "parties" });
  const coverages = useFieldArray({ control: form.control, name: "coverages" });
  const items = useFieldArray({ control: form.control, name: "items" });

  const lineId = useWatch({ control: form.control, name: "lineOfBusinessId" });
  const insurerId = useWatch({ control: form.control, name: "insurerId" });
  const schemaKey = lines.data?.find((l) => l.id === lineId)?.riskSchemaKey;
  const fields = riskFields(schemas.data?.find((s) => s.key === schemaKey)?.schema);
  const productOptions = products.data?.filter(
    (p) => p.active && p.insurerId === insurerId && p.lineOfBusinessId === lineId,
  );

  const save = useMutation({
    mutationFn: async (v: PolicyFormValues) => {
      const terms: PolicyTerms = {
        insurerId: v.insurerId,
        lineOfBusinessId: v.lineOfBusinessId,
        ...(v.productId ? { productId: v.productId } : {}),
        ...(v.agentId ? { agentId: v.agentId } : {}),
        currency: v.currency,
        netPremium: v.netPremium,
        taxes: v.taxes,
        effectiveFrom: v.effectiveFrom,
        effectiveTo: v.effectiveTo,
        paymentForm: v.paymentForm,
        parties: [{ partyId: v.holderId, role: "holder" }, ...v.parties],
        coverages: v.coverages.map((c) => ({
          name: c.name,
          sumInsured: c.sumInsured,
          ...(c.deductible ? { deductible: c.deductible } : {}),
        })),
        items: v.items.map((i) => ({
          description: i.description,
          riskData: toRiskData(fields, i.risk),
        })),
      };
      if (policy) {
        const res = await updatePolicyTerms(policy.id, terms, { version: policy.version });
        if (res.status !== 200) throw new Error("no guardada");
        return res.data;
      }
      const res = await createPolicy(terms);
      if (res.status !== 201) throw new Error("no creada");
      return res.data;
    },
    onSuccess: async (saved) => {
      await queryClient.invalidateQueries({ queryKey: ["policies"] });
      await navigate({ to: "/polizas/$policyId", params: { policyId: saved.id } });
    },
  });

  const { errors } = form.formState;
  return (
    <section aria-labelledby="policy-form-title" className="max-w-3xl space-y-6">
      <h1 id="policy-form-title" className="text-2xl font-semibold">
        {policy ? "Ajustar póliza" : "Nueva póliza"}
      </h1>
      <form
        noValidate
        className="space-y-8"
        onSubmit={(e) => {
          void form.handleSubmit((v) => {
            save.mutate(v);
          })(e);
        }}
      >
        <ErrorMessage message={problemMessage(save.error)} />

        <fieldset className="space-y-4">
          <legend className="text-lg font-semibold">Aseguradora y ramo</legend>
          <div className="grid gap-4 sm:grid-cols-3">
            <Field label="Aseguradora" error={errors.insurerId?.message}>
              {(p) => (
                <Select {...p} {...form.register("insurerId")}>
                  <option value="">Selecciona…</option>
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
            <Field label="Ramo" error={errors.lineOfBusinessId?.message}>
              {(p) => (
                <Select {...p} {...form.register("lineOfBusinessId")}>
                  <option value="">Selecciona…</option>
                  {lines.data
                    ?.filter((l) => l.active)
                    .map((l) => (
                      <option key={l.id} value={l.id}>
                        {l.name}
                      </option>
                    ))}
                </Select>
              )}
            </Field>
            <Field label="Producto (opcional)">
              {(p) => (
                <Select {...p} {...form.register("productId")}>
                  <option value="">Sin producto</option>
                  {productOptions?.map((x) => (
                    <option key={x.id} value={x.id}>
                      {x.name}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          </div>
        </fieldset>

        <fieldset className="space-y-4">
          <legend className="text-lg font-semibold">Partes</legend>
          <Controller
            control={form.control}
            name="holderId"
            render={({ field }) => (
              <deps.PartyPicker
                label="Tomador"
                value={field.value}
                onChange={field.onChange}
                error={errors.holderId?.message}
              />
            )}
          />
          {parties.fields.map((row, i) => (
            <div key={row.id} className="space-y-2 rounded border p-3">
              <Controller
                control={form.control}
                name={`parties.${i}.partyId`}
                render={({ field }) => (
                  <deps.PartyPicker
                    label={`Cliente ${String(i + 1)}`}
                    value={field.value}
                    onChange={field.onChange}
                    error={errors.parties?.[i]?.partyId?.message}
                  />
                )}
              />
              <div className="flex flex-wrap items-end gap-2">
                <Field label={`Papel del cliente ${String(i + 1)}`}>
                  {(p) => (
                    <Select {...p} {...form.register(`parties.${i}.role`)}>
                      <option value="insured">{ROLE_LABELS.insured}</option>
                      <option value="beneficiary">{ROLE_LABELS.beneficiary}</option>
                    </Select>
                  )}
                </Field>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    parties.remove(i);
                  }}
                >
                  Quitar
                </Button>
              </div>
            </div>
          ))}
          {errors.parties?.message && (
            <p className="text-sm text-destructive">{errors.parties.message}</p>
          )}
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              parties.append({ partyId: "", role: "beneficiary" });
            }}
          >
            Agregar asegurado o beneficiario
          </Button>
        </fieldset>

        <fieldset className="space-y-4">
          <legend className="text-lg font-semibold">Prima y vigencia</legend>
          <p className="text-sm text-muted-foreground">
            Registra los valores que informó la aseguradora. La vigencia incluye el último día.
          </p>
          <div className="grid gap-4 sm:grid-cols-3">
            <Field label="Moneda" error={errors.currency?.message}>
              {(p) => (
                <Select {...p} {...form.register("currency")}>
                  <option value="COP">COP</option>
                  <option value="USD">USD</option>
                  <option value="EUR">EUR</option>
                </Select>
              )}
            </Field>
            <Field label="Prima neta" error={errors.netPremium?.message}>
              {(p) => <Input {...p} inputMode="decimal" {...form.register("netPremium")} />}
            </Field>
            <Field label="Impuestos y gastos" error={errors.taxes?.message}>
              {(p) => <Input {...p} inputMode="decimal" {...form.register("taxes")} />}
            </Field>
            <Field label="Vigente desde" error={errors.effectiveFrom?.message}>
              {(p) => <Input {...p} type="date" {...form.register("effectiveFrom")} />}
            </Field>
            <Field label="Vigente hasta" error={errors.effectiveTo?.message}>
              {(p) => <Input {...p} type="date" {...form.register("effectiveTo")} />}
            </Field>
            <Field label="Forma de pago" error={errors.paymentForm?.message}>
              {(p) => (
                <Select {...p} {...form.register("paymentForm")}>
                  {Object.entries(PAYMENT_LABELS).map(([value, label]) => (
                    <option key={value} value={value}>
                      {label}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
            <Field label="Asesor (opcional)">
              {(p) => (
                <Select {...p} {...form.register("agentId")}>
                  <option value="">Sin asesor</option>
                  {agents.data
                    ?.filter((a) => a.active || a.id === policy?.agentId)
                    .map((a) => (
                      <option key={a.id} value={a.id}>
                        {a.displayName}
                      </option>
                    ))}
                </Select>
              )}
            </Field>
          </div>
        </fieldset>

        <fieldset className="space-y-4">
          <legend className="text-lg font-semibold">Coberturas</legend>
          {coverages.fields.map((row, i) => (
            <div key={row.id} className="grid items-end gap-2 sm:grid-cols-[1fr_1fr_1fr_auto]">
              <Field
                label={`Cobertura ${String(i + 1)}`}
                error={errors.coverages?.[i]?.name?.message}
              >
                {(p) => <Input {...p} {...form.register(`coverages.${i}.name`)} />}
              </Field>
              <Field
                label={`Suma asegurada ${String(i + 1)}`}
                error={errors.coverages?.[i]?.sumInsured?.message}
              >
                {(p) => (
                  <Input
                    {...p}
                    inputMode="decimal"
                    {...form.register(`coverages.${i}.sumInsured`)}
                  />
                )}
              </Field>
              <Field
                label={`Deducible ${String(i + 1)}`}
                error={errors.coverages?.[i]?.deductible?.message}
              >
                {(p) => <Input {...p} {...form.register(`coverages.${i}.deductible`)} />}
              </Field>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  coverages.remove(i);
                }}
              >
                Quitar
              </Button>
            </div>
          ))}
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              coverages.append({ name: "", sumInsured: "", deductible: "" });
            }}
          >
            Agregar cobertura
          </Button>
        </fieldset>

        <fieldset className="space-y-4">
          <legend className="text-lg font-semibold">Bienes o personas aseguradas</legend>
          {!schemaKey && (
            <p className="text-sm text-muted-foreground">Selecciona el ramo para ver sus datos.</p>
          )}
          {items.fields.map((row, i) => (
            <div key={row.id} className="space-y-3 rounded border p-3">
              <Field
                label={`Descripción del bien ${String(i + 1)}`}
                error={errors.items?.[i]?.description?.message}
              >
                {(p) => <Input {...p} {...form.register(`items.${i}.description`)} />}
              </Field>
              <Controller
                control={form.control}
                name={`items.${i}.risk`}
                render={({ field }) => (
                  <RiskFields
                    fields={fields}
                    values={field.value}
                    suffix={items.fields.length > 1 ? ` (bien ${String(i + 1)})` : ""}
                    onChange={(name, value) => {
                      field.onChange({ ...field.value, [name]: value });
                    }}
                  />
                )}
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  items.remove(i);
                }}
              >
                Quitar
              </Button>
            </div>
          ))}
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              items.append({ description: "", risk: {} });
            }}
          >
            Agregar bien
          </Button>
        </fieldset>

        <Button type="submit" disabled={save.isPending}>
          {policy ? "Guardar cambios" : "Guardar póliza"}
        </Button>
      </form>
    </section>
  );
}
