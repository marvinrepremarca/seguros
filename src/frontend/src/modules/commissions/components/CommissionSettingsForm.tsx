import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { formatMoney } from "@/shared/lib/format";
import { problemMessage } from "@/shared/lib/problem";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input } from "@/shared/ui/field";

import { getCommissionSettings, setCommissionSettings } from "../api/generated/commissions";

/** Tolerancia de conciliación por moneda (DECISIONS T4, N26). */
export function CommissionSettingsForm() {
  const client = useQueryClient();
  const [currency, setCurrency] = useState("COP");
  const [tolerance, setTolerance] = useState("");
  const query = useQuery({
    queryKey: ["commissions", "settings"],
    queryFn: async () => {
      const res = await getCommissionSettings();
      if (res.status !== 200) throw new Error("no disponibles");
      return res.data;
    },
  });
  const save = useMutation({
    mutationFn: async () => {
      const res = await setCommissionSettings({ currency, tolerance });
      if (res.status !== 204) throw new Error("no guardado");
    },
    onSuccess: () => client.invalidateQueries({ queryKey: ["commissions", "settings"] }),
  });
  return (
    <form
      className="max-w-sm space-y-2"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <ul className="text-sm">
        {query.data?.map((t) => (
          <li key={t.currency}>
            {t.currency}: {formatMoney(t.tolerance, t.currency)} por póliza
          </li>
        ))}
      </ul>
      <Field label="Moneda" hint="Sin tolerancia fijada, otra moneda exige coincidencia exacta.">
        {(p) => (
          <Input
            {...p}
            required
            maxLength={3}
            value={currency}
            onChange={(e) => {
              setCurrency(e.target.value.toUpperCase());
            }}
          />
        )}
      </Field>
      <Field label="Tolerancia de conciliación por póliza">
        {(p) => (
          <Input
            {...p}
            required
            inputMode="decimal"
            value={tolerance}
            onChange={(e) => {
              setTolerance(e.target.value);
            }}
          />
        )}
      </Field>
      <ErrorMessage message={problemMessage(save.error)} />
      <Button type="submit" variant="outline" disabled={save.isPending}>
        Guardar
      </Button>
    </form>
  );
}
