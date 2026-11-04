import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type SyntheticEvent, type ReactNode } from "react";

import { formatDate } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { permissions, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState, Select } from "@/shared/ui/field";

import {
  createCommissionAgreement,
  createInsurer,
  createLineOfBusiness,
  createProduct,
  listCommissionAgreements,
  updateInsurer,
  updateLineOfBusiness,
  updateProduct,
  type CommissionBasis,
  type RiskSchemaKey,
} from "../api/generated/catalog";
import { insurersQuery, linesQuery, productsQuery, riskSchemasQuery } from "../queries";

const INVALIDATE = { queryKey: ["catalog"] };

function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-3 rounded-lg border p-4">
      <h2 className="text-lg font-semibold">{title}</h2>
      {children}
    </section>
  );
}

function submit(fn: () => void) {
  return (e: SyntheticEvent) => {
    e.preventDefault();
    fn();
  };
}

interface Row {
  id: string;
  name: string;
  active: boolean;
  version: number;
  detail?: string | undefined;
}

function ItemList({
  label,
  rows,
  onToggle,
  canManage,
}: {
  label: string;
  rows: readonly Row[];
  onToggle: (row: Row) => void;
  canManage: boolean;
}) {
  if (rows.length === 0) return <p className="text-sm text-muted-foreground">Sin registros.</p>;
  return (
    <ul className="divide-y text-sm">
      {rows.map((r) => (
        <li key={r.id} className="flex flex-wrap items-center justify-between gap-2 py-2">
          <span className={r.active ? "" : "text-muted-foreground line-through"}>
            {r.name}
            {r.detail && <span className="text-muted-foreground"> · {r.detail}</span>}
          </span>
          {canManage && (
            <Button
              variant="outline"
              size="sm"
              aria-label={`${r.active ? "Desactivar" : "Activar"} ${label} ${r.name}`}
              onClick={() => {
                onToggle(r);
              }}
            >
              {r.active ? "Desactivar" : "Activar"}
            </Button>
          )}
        </li>
      ))}
    </ul>
  );
}

function InsurersPanel({ canManage }: { canManage: boolean }) {
  const queryClient = useQueryClient();
  const insurers = useQuery(insurersQuery());
  const [name, setName] = useState("");
  const invalidate = () => queryClient.invalidateQueries(INVALIDATE);
  const create = useMutation({
    mutationFn: () => createInsurer({ name }),
    onSuccess: () => {
      setName("");
      return invalidate();
    },
  });
  const toggle = useMutation({
    mutationFn: (r: Row) =>
      updateInsurer(r.id, { name: r.name, active: !r.active, version: r.version }),
    onSuccess: invalidate,
  });
  return (
    <Panel title="Aseguradoras">
      <QueryState
        isPending={insurers.isPending}
        isError={insurers.isError}
        isEmpty={false}
        emptyMessage=""
      />
      <ErrorMessage message={problemMessage(create.error ?? toggle.error)} />
      <ItemList
        label="aseguradora"
        rows={insurers.data ?? []}
        canManage={canManage}
        onToggle={(r) => {
          toggle.mutate(r);
        }}
      />
      {canManage && (
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={submit(() => {
            create.mutate();
          })}
        >
          <div className="grow">
            <Field label="Nueva aseguradora">
              {(p) => (
                <Input
                  {...p}
                  required
                  minLength={2}
                  maxLength={120}
                  value={name}
                  onChange={(e) => {
                    setName(e.target.value);
                  }}
                />
              )}
            </Field>
          </div>
          <Button type="submit" disabled={create.isPending}>
            Agregar aseguradora
          </Button>
        </form>
      )}
    </Panel>
  );
}

function LinesPanel({ canManage }: { canManage: boolean }) {
  const queryClient = useQueryClient();
  const lines = useQuery(linesQuery());
  const schemas = useQuery(riskSchemasQuery());
  const [name, setName] = useState("");
  const [schemaKey, setSchemaKey] = useState<RiskSchemaKey>("generic");
  const invalidate = () => queryClient.invalidateQueries(INVALIDATE);
  const create = useMutation({
    mutationFn: () => createLineOfBusiness({ name, riskSchemaKey: schemaKey }),
    onSuccess: () => {
      setName("");
      return invalidate();
    },
  });
  const toggle = useMutation({
    mutationFn: (r: Row) =>
      updateLineOfBusiness(r.id, { name: r.name, active: !r.active, version: r.version }),
    onSuccess: invalidate,
  });
  const label = (key: string) => schemas.data?.find((s) => s.key === key)?.label ?? key;
  return (
    <Panel title="Ramos">
      <ErrorMessage message={problemMessage(create.error ?? toggle.error)} />
      <ItemList
        label="ramo"
        rows={(lines.data ?? []).map((l) => ({
          ...l,
          detail: `Datos de ${label(l.riskSchemaKey)}`,
        }))}
        canManage={canManage}
        onToggle={(r) => {
          toggle.mutate(r);
        }}
      />
      {canManage && (
        <form
          className="grid items-end gap-2 sm:grid-cols-[1fr_1fr_auto]"
          onSubmit={submit(() => {
            create.mutate();
          })}
        >
          <Field label="Nuevo ramo">
            {(p) => (
              <Input
                {...p}
                required
                minLength={2}
                maxLength={120}
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                }}
              />
            )}
          </Field>
          <Field label="Datos de riesgo" hint="No se puede cambiar después">
            {(p) => (
              <Select
                {...p}
                value={schemaKey}
                onChange={(e) => {
                  const found = schemas.data?.find((s) => s.key === e.target.value);
                  if (found) setSchemaKey(found.key);
                }}
              >
                {schemas.data?.map((s) => (
                  <option key={s.key} value={s.key}>
                    {s.label}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Button type="submit" disabled={create.isPending}>
            Agregar ramo
          </Button>
        </form>
      )}
    </Panel>
  );
}

function ProductsPanel({ canManage }: { canManage: boolean }) {
  const queryClient = useQueryClient();
  const products = useQuery(productsQuery());
  const insurers = useQuery(insurersQuery());
  const lines = useQuery(linesQuery());
  const [name, setName] = useState("");
  const [insurerId, setInsurerId] = useState("");
  const [lineId, setLineId] = useState("");
  const invalidate = () => queryClient.invalidateQueries(INVALIDATE);
  const create = useMutation({
    mutationFn: () => createProduct({ name, insurerId, lineOfBusinessId: lineId }),
    onSuccess: () => {
      setName("");
      return invalidate();
    },
  });
  const toggle = useMutation({
    mutationFn: (r: Row) =>
      updateProduct(r.id, { name: r.name, active: !r.active, version: r.version }),
    onSuccess: invalidate,
  });
  const nameOf = (list: readonly { id: string; name: string }[] | undefined, id: string) =>
    list?.find((x) => x.id === id)?.name ?? "";
  return (
    <Panel title="Productos">
      <ErrorMessage message={problemMessage(create.error ?? toggle.error)} />
      <ItemList
        label="producto"
        rows={(products.data ?? []).map((x) => ({
          ...x,
          detail: `${nameOf(insurers.data, x.insurerId)} · ${nameOf(lines.data, x.lineOfBusinessId)}`,
        }))}
        canManage={canManage}
        onToggle={(r) => {
          toggle.mutate(r);
        }}
      />
      {canManage && (
        <form
          className="grid items-end gap-2 sm:grid-cols-[1fr_1fr_1fr_auto]"
          onSubmit={submit(() => {
            create.mutate();
          })}
        >
          <Field label="Nuevo producto">
            {(p) => (
              <Input
                {...p}
                required
                minLength={2}
                maxLength={120}
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                }}
              />
            )}
          </Field>
          <Field label="Aseguradora del producto">
            {(p) => (
              <Select
                {...p}
                required
                value={insurerId}
                onChange={(e) => {
                  setInsurerId(e.target.value);
                }}
              >
                <option value="">Selecciona…</option>
                {insurers.data?.map((i) => (
                  <option key={i.id} value={i.id}>
                    {i.name}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="Ramo del producto">
            {(p) => (
              <Select
                {...p}
                required
                value={lineId}
                onChange={(e) => {
                  setLineId(e.target.value);
                }}
              >
                <option value="">Selecciona…</option>
                {lines.data?.map((l) => (
                  <option key={l.id} value={l.id}>
                    {l.name}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Button type="submit" disabled={create.isPending}>
            Agregar producto
          </Button>
        </form>
      )}
    </Panel>
  );
}

const BASIS_LABELS: Readonly<Record<CommissionBasis, string>> = {
  paid_premium: "Prima pagada",
  written_premium: "Prima emitida",
};

function AgreementsPanel({ canManage }: { canManage: boolean }) {
  const queryClient = useQueryClient();
  const insurers = useQuery(insurersQuery());
  const lines = useQuery(linesQuery());
  const agreements = useQuery({
    queryKey: ["catalog", "agreements"],
    queryFn: async () => {
      const res = await listCommissionAgreements({ limit: 200 });
      if (res.status !== 200) throw new Error("acuerdos no disponibles");
      return res.data.items;
    },
  });
  const [insurerId, setInsurerId] = useState("");
  const [lineId, setLineId] = useState("");
  const [percentage, setPercentage] = useState("");
  const [validFrom, setValidFrom] = useState("");
  const [validTo, setValidTo] = useState("");
  const [basis, setBasis] = useState<CommissionBasis>("paid_premium");
  const create = useMutation({
    mutationFn: () =>
      createCommissionAgreement({
        insurerId,
        lineOfBusinessId: lineId,
        basis,
        percentage,
        validFrom,
        validTo,
      }),
    onSuccess: () => {
      setPercentage("");
      return queryClient.invalidateQueries(INVALIDATE);
    },
  });
  const nameOf = (list: readonly { id: string; name: string }[] | undefined, id: string) =>
    list?.find((x) => x.id === id)?.name ?? "";
  return (
    <Panel title="Acuerdos de comisión">
      <ErrorMessage message={problemMessage(create.error)} />
      <QueryState
        isPending={agreements.isPending}
        isError={agreements.isError}
        isEmpty={agreements.data?.length === 0}
        emptyMessage="Sin acuerdos registrados."
      />
      {agreements.data !== undefined && agreements.data.length > 0 && (
        <ul className="divide-y text-sm">
          {agreements.data.map((a) => (
            <li key={a.id} className="py-2">
              {nameOf(insurers.data, a.insurerId)} · {nameOf(lines.data, a.lineOfBusinessId)}:{" "}
              <strong>{a.percentage} %</strong> sobre {BASIS_LABELS[a.basis].toLowerCase()}, del{" "}
              {formatDate(a.validFrom)} al {formatDate(a.validTo)}
            </li>
          ))}
        </ul>
      )}
      {canManage && (
        <form
          className="grid items-end gap-2 sm:grid-cols-3"
          onSubmit={submit(() => {
            create.mutate();
          })}
        >
          <Field label="Aseguradora del acuerdo">
            {(p) => (
              <Select
                {...p}
                required
                value={insurerId}
                onChange={(e) => {
                  setInsurerId(e.target.value);
                }}
              >
                <option value="">Selecciona…</option>
                {insurers.data?.map((i) => (
                  <option key={i.id} value={i.id}>
                    {i.name}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="Ramo del acuerdo">
            {(p) => (
              <Select
                {...p}
                required
                value={lineId}
                onChange={(e) => {
                  setLineId(e.target.value);
                }}
              >
                <option value="">Selecciona…</option>
                {lines.data?.map((l) => (
                  <option key={l.id} value={l.id}>
                    {l.name}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="Porcentaje">
            {(p) => (
              <Input
                {...p}
                required
                inputMode="decimal"
                pattern="\d{1,3}(\.\d{1,6})?"
                value={percentage}
                onChange={(e) => {
                  setPercentage(e.target.value);
                }}
              />
            )}
          </Field>
          <Field label="Base">
            {(p) => (
              <Select
                {...p}
                value={basis}
                onChange={(e) => {
                  setBasis(
                    e.target.value === "written_premium" ? "written_premium" : "paid_premium",
                  );
                }}
              >
                <option value="paid_premium">{BASIS_LABELS.paid_premium}</option>
                <option value="written_premium">{BASIS_LABELS.written_premium}</option>
              </Select>
            )}
          </Field>
          <Field label="Vigente desde">
            {(p) => (
              <Input
                {...p}
                required
                type="date"
                value={validFrom}
                onChange={(e) => {
                  setValidFrom(e.target.value);
                }}
              />
            )}
          </Field>
          <Field label="Vigente hasta">
            {(p) => (
              <Input
                {...p}
                required
                type="date"
                value={validTo}
                onChange={(e) => {
                  setValidTo(e.target.value);
                }}
              />
            )}
          </Field>
          <Button type="submit" disabled={create.isPending}>
            Agregar acuerdo
          </Button>
        </form>
      )}
    </Panel>
  );
}

export function CatalogPage() {
  const canManage = usePermission(permissions.catalogManage);
  const canReadAgreements = usePermission(permissions.agreementRead);
  const canManageAgreements = usePermission(permissions.agreementManage);
  return (
    <section aria-labelledby="catalog-title" className="space-y-4">
      <h1 id="catalog-title" className="text-2xl font-semibold">
        Catálogo
      </h1>
      <InsurersPanel canManage={canManage} />
      <LinesPanel canManage={canManage} />
      <ProductsPanel canManage={canManage} />
      {canReadAgreements && <AgreementsPanel canManage={canManageAgreements} />}
    </section>
  );
}
