import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useForm } from "react-hook-form";

import { problemMessage } from "@/shared/lib/problem";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, Select } from "@/shared/ui/field";

import { createParty, type CreatePartyRequest } from "../api/generated/parties";
import { DOCUMENT_LABELS, partyFormSchema, type PartyFormValues } from "../schemas";

function toRequest(v: PartyFormValues): CreatePartyRequest {
  return {
    kind: v.kind,
    name: v.name,
    documentType: v.documentType,
    documentNumber: v.documentNumber,
    ...(v.email ? { email: v.email } : {}),
    ...(v.phone ? { phone: v.phone } : {}),
    ...(v.address ? { address: v.address } : {}),
    ...(v.city ? { city: v.city } : {}),
  };
}

export function NewPartyPage() {
  const navigate = useNavigate();
  const form = useForm({
    resolver: zodResolver(partyFormSchema),
    defaultValues: {
      kind: "person",
      name: "",
      documentType: "CC",
      documentNumber: "",
      email: "",
      phone: "",
      address: "",
      city: "",
    } satisfies PartyFormValues,
  });
  const create = useMutation({
    mutationFn: async (v: PartyFormValues) => {
      const res = await createParty(toRequest(v));
      if (res.status !== 201) throw new Error("no creado");
      return res.data;
    },
    onSuccess: (party) => {
      void navigate({ to: "/clientes/$partyId", params: { partyId: party.id } });
    },
  });
  const { errors } = form.formState;

  return (
    <section aria-labelledby="new-party-title" className="max-w-xl space-y-4">
      <h1 id="new-party-title" className="text-2xl font-semibold">
        Nuevo cliente
      </h1>
      <form
        noValidate
        className="space-y-4"
        onSubmit={(e) => {
          void form.handleSubmit((v) => {
            create.mutate(v);
          })(e);
        }}
      >
        <ErrorMessage message={problemMessage(create.error)} />
        <Field label="Tipo de cliente" error={errors.kind?.message}>
          {(p) => (
            <Select {...p} {...form.register("kind")}>
              <option value="person">Persona</option>
              <option value="organization">Organización</option>
            </Select>
          )}
        </Field>
        <Field label="Nombre o razón social" error={errors.name?.message}>
          {(p) => <Input {...p} autoComplete="off" {...form.register("name")} />}
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Tipo de documento" error={errors.documentType?.message}>
            {(p) => (
              <Select {...p} {...form.register("documentType")}>
                {Object.entries(DOCUMENT_LABELS).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="Número de documento" error={errors.documentNumber?.message}>
            {(p) => <Input {...p} autoComplete="off" {...form.register("documentNumber")} />}
          </Field>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
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
        </div>
        <Button type="submit" disabled={create.isPending}>
          Guardar cliente
        </Button>
      </form>
    </section>
  );
}
