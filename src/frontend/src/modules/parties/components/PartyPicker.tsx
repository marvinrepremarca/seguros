import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { Field, Input, Select } from "@/shared/ui/field";

import { listParties } from "../api/generated/parties";

interface PartyPickerProps {
  label: string;
  value: string;
  onChange: (partyId: string) => void;
  error?: string | undefined;
}

/** Busca un cliente por nombre y lo selecciona. */
export function PartyPicker({ label, value, onChange, error }: PartyPickerProps) {
  const [search, setSearch] = useState("");
  const q = search.trim();
  const results = useQuery({
    queryKey: ["parties", "picker", q],
    enabled: q.length >= 2,
    queryFn: async () => {
      const res = await listParties({ q, limit: 20 });
      if (res.status !== 200) throw new Error("clientes no disponibles");
      return res.data.items;
    },
  });
  return (
    <div className="grid gap-2 sm:grid-cols-2">
      <Field label={`Buscar ${label.toLowerCase()}`} hint="Escribe al menos 2 letras">
        {(p) => (
          <Input
            {...p}
            type="search"
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
            }}
          />
        )}
      </Field>
      <Field label={label} error={error}>
        {(p) => (
          <Select
            {...p}
            value={value}
            onChange={(e) => {
              onChange(e.target.value);
            }}
          >
            <option value="">{value ? "Cliente seleccionado" : "Selecciona…"}</option>
            {results.data?.map((party) => (
              <option key={party.id} value={party.id}>
                {party.name} ({party.documentType} •••• {party.documentLast4})
              </option>
            ))}
          </Select>
        )}
      </Field>
    </div>
  );
}
