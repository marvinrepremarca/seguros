import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type SubmitEvent } from "react";

import { features, useFeature } from "@/shared/lib/features";
import { formatDate } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { permissions, safeExternalUrl, usePermission } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState, Select } from "@/shared/ui/field";

import {
  addDocumentVersion,
  downloadDocumentVersion,
  getDocument,
  listDocuments,
  listRetentionDue,
  uploadDocument,
  type Document,
  type OwnerType,
  type ScanStatus,
} from "../api/generated/documents";
import type { DocumentsDeps } from "../deps";

const SCAN_LABELS: Record<ScanStatus, string> = {
  pending: "En revisión antivirus",
  clean: "Disponible",
  infected: "Bloqueado por antivirus",
};

function OwnerPicker({
  deps,
  type,
  value,
  onChange,
}: {
  deps: DocumentsDeps;
  type: OwnerType;
  value: string;
  onChange: (id: string) => void;
}) {
  const canPolicy = usePermission(permissions.policyRead);
  const canClaim = usePermission(permissions.claimRead);
  const claimsOn = useFeature(features.claims.module);
  const policies = useQuery({ ...deps.policies, enabled: type === "policy" && canPolicy });
  const claims = useQuery({ ...deps.claims, enabled: type === "claim" && canClaim && claimsOn });
  if (type === "party") {
    return <deps.PartyPicker label="Cliente" value={value} onChange={onChange} />;
  }
  const options = type === "policy" ? policies.data : claims.data;
  return (
    <Field label={type === "policy" ? "Póliza" : "Siniestro"}>
      {(p) => (
        <Select
          {...p}
          value={value}
          onChange={(e) => {
            onChange(e.target.value);
          }}
        >
          <option value="">Selecciona…</option>
          {options?.map((o) => (
            <option key={o.id} value={o.id}>
              {o.label}
            </option>
          ))}
        </Select>
      )}
    </Field>
  );
}

function DocumentDetail({ doc }: { doc: Document }) {
  const qc = useQueryClient();
  const canUpload = usePermission(permissions.documentUpload);
  const key = ["documents", "detail", doc.id];
  const detail = useQuery({
    queryKey: key,
    queryFn: async () => {
      const res = await getDocument(doc.id);
      if (res.status !== 200) throw new Error("documento no disponible");
      return res.data;
    },
  });
  const download = useMutation({
    mutationFn: async (version: string) => {
      const res = await downloadDocumentVersion(doc.id, version);
      if (res.status !== 200) throw new Error("descarga no disponible");
      return res.data.url;
    },
    onSuccess: (url) => {
      // Los archivos cifrados se sirven desde la propia API, con ruta relativa.
      const safe = safeExternalUrl(new URL(url, window.location.origin).href);
      if (safe) window.location.assign(safe);
    },
  });
  const add = useMutation({
    mutationFn: async (file: File) => {
      const res = await addDocumentVersion(doc.id, { file });
      if (res.status !== 201) throw new Error("versión no cargada");
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  });
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    const file = new FormData(e.currentTarget).get("file");
    if (file instanceof File) add.mutate(file);
  };
  return (
    <div className="mt-2 space-y-2">
      <QueryState
        isPending={detail.isPending}
        isError={detail.isError}
        isEmpty={false}
        emptyMessage=""
      />
      <ul className="space-y-1">
        {detail.data?.versions.map((v) => (
          <li key={v.id} className="flex flex-wrap items-center gap-2">
            Versión {v.number} · {SCAN_LABELS[v.scanStatus]}
            {v.scanStatus === "clean" && (
              <Button
                variant="outline"
                disabled={download.isPending}
                onClick={() => {
                  download.mutate(v.id);
                }}
              >
                Descargar
              </Button>
            )}
          </li>
        ))}
      </ul>
      {canUpload && (
        <form onSubmit={submit} className="flex flex-wrap items-end gap-2">
          <Field label="Nueva versión">
            {(p) => (
              <Input
                {...p}
                name="file"
                type="file"
                required
                accept="application/pdf,image/png,image/jpeg"
              />
            )}
          </Field>
          <Button type="submit" disabled={add.isPending}>
            Cargar versión
          </Button>
        </form>
      )}
      <ErrorMessage message={problemMessage(download.error ?? add.error)} />
    </div>
  );
}

function DocumentRow({ doc }: { doc: Document }) {
  const [open, setOpen] = useState(false);
  return (
    <li className="rounded-md border p-3 text-sm">
      <button
        type="button"
        className="underline underline-offset-2"
        aria-expanded={open}
        onClick={() => {
          setOpen(!open);
        }}
      >
        {doc.title}
      </button>{" "}
      · {formatDate(doc.createdOn)}
      {doc.sensitive && " · Sensible"} · revisión de retención: {formatDate(doc.retentionUntil)}
      {open && <DocumentDetail doc={doc} />}
    </li>
  );
}

function OwnerDocuments({ type, owner }: { type: OwnerType; owner: string }) {
  const qc = useQueryClient();
  const canUpload = usePermission(permissions.documentUpload);
  const canSensitive = usePermission(permissions.documentUploadSensitive);
  const key = ["documents", "list", type, owner];
  const list = useQuery({
    queryKey: key,
    queryFn: async () => {
      const res = await listDocuments({ ownerType: type, ownerId: owner, limit: 100 });
      if (res.status !== 200) throw new Error("documentos no disponibles");
      return res.data.items;
    },
  });
  const upload = useMutation({
    mutationFn: async (f: FormData) => {
      const file = f.get("file");
      const title = f.get("title");
      if (!(file instanceof File) || typeof title !== "string")
        throw new Error("archivo requerido");
      const res = await uploadDocument(
        { file, ownerType: type, ownerId: owner, title, sensitive: f.get("sensitive") === "on" },
        { headers: { "Idempotency-Key": crypto.randomUUID() } },
      );
      if (res.status !== 201) throw new Error("documento no cargado");
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  });
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    upload.mutate(new FormData(e.currentTarget));
  };
  return (
    <div className="space-y-3">
      {canUpload && (
        <form onSubmit={submit} className="grid gap-3 rounded-md border p-3 sm:grid-cols-2">
          <Field label="Título">
            {(p) => <Input {...p} name="title" required minLength={2} maxLength={200} />}
          </Field>
          <Field label="Archivo" hint="PDF, PNG o JPEG. Se habilita tras la revisión antivirus.">
            {(p) => (
              <Input
                {...p}
                name="file"
                type="file"
                required
                accept="application/pdf,image/png,image/jpeg"
              />
            )}
          </Field>
          {canSensitive && type !== "claim" && (
            <label className="flex items-center gap-2 text-sm sm:col-span-2">
              <input type="checkbox" name="sensitive" />
              Contiene datos sensibles (salud, SARLAFT)
            </label>
          )}
          <div className="sm:col-span-2">
            <ErrorMessage message={problemMessage(upload.error)} />
            <Button type="submit" disabled={upload.isPending}>
              Cargar documento
            </Button>
          </div>
        </form>
      )}
      <QueryState
        isPending={list.isPending}
        isError={list.isError}
        isEmpty={list.data?.length === 0}
        emptyMessage="No hay documentos."
      />
      <ul className="space-y-2">
        {list.data?.map((d) => (
          <DocumentRow key={d.id} doc={d} />
        ))}
      </ul>
    </div>
  );
}

function RetentionSection() {
  const list = useQuery({
    queryKey: ["documents", "retention"],
    queryFn: async () => {
      const res = await listRetentionDue({ limit: 100 });
      if (res.status !== 200) throw new Error("revisión no disponible");
      return res.data.items;
    },
  });
  return (
    <section aria-labelledby="retention-title" className="space-y-3">
      <h2 id="retention-title" className="text-lg font-medium">
        Revisión de retención
      </h2>
      <p className="text-sm text-muted-foreground">
        Documentos con el plazo cumplido. El sistema no borra nada de forma automática.
      </p>
      <QueryState
        isPending={list.isPending}
        isError={list.isError}
        isEmpty={list.data?.length === 0}
        emptyMessage="Ningún documento ha cumplido el plazo."
      />
      <ul className="space-y-2">
        {list.data?.map((d) => (
          <DocumentRow key={d.id} doc={d} />
        ))}
      </ul>
    </section>
  );
}

/** Gestión documental. */
export function DocumentsPage({ deps }: { deps: DocumentsDeps }) {
  const [type, setType] = useState<OwnerType>("party");
  const [owner, setOwner] = useState("");
  const canRetention = usePermission(permissions.documentRetention);
  return (
    <section aria-labelledby="documents-title" className="space-y-6">
      <h1 id="documents-title" className="text-2xl font-semibold">
        Documentos
      </h1>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="Pertenece a">
          {(p) => (
            <Select
              {...p}
              value={type}
              onChange={(e) => {
                const v = e.target.value;
                if (v === "party" || v === "policy" || v === "claim") {
                  setType(v);
                  setOwner("");
                }
              }}
            >
              <option value="party">Cliente</option>
              <option value="policy">Póliza</option>
              <option value="claim">Siniestro</option>
            </Select>
          )}
        </Field>
        <OwnerPicker deps={deps} type={type} value={owner} onChange={setOwner} />
      </div>
      {owner && <OwnerDocuments key={`${type}-${owner}`} type={type} owner={owner} />}
      {canRetention && <RetentionSection />}
    </section>
  );
}
