import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { problemMessage } from "@/shared/lib/problem";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState } from "@/shared/ui/field";

import {
  getPaymentSettings,
  listArrearsSettings,
  setArrearsSetting,
  setPaymentSettings,
} from "../api/generated/billing";

function NumberForm({
  label,
  hint,
  initial,
  onSave,
}: {
  label: string;
  hint: string;
  initial: number;
  onSave: (value: number) => Promise<unknown>;
}) {
  const client = useQueryClient();
  const [value, setValue] = useState(String(initial));
  const save = useMutation({
    mutationFn: () => onSave(Number(value)),
    onSuccess: () => client.invalidateQueries({ queryKey: ["billing", "settings"] }),
  });
  return (
    <form
      className="max-w-sm space-y-2"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <Field label={label} hint={hint}>
        {(p) => (
          <Input
            {...p}
            required
            type="number"
            min={0}
            value={value}
            onChange={(e) => {
              setValue(e.target.value);
            }}
          />
        )}
      </Field>
      <ErrorMessage message={problemMessage(save.error)} />
      {save.isSuccess && <p role="status">Guardado.</p>}
      <Button type="submit" variant="outline" disabled={save.isPending}>
        Guardar
      </Button>
    </form>
  );
}

/** Parámetros de cartera de la agencia (DECISIONS N2, N22). */
export function BillingSettings() {
  const query = useQuery({
    queryKey: ["billing", "settings"],
    queryFn: async () => {
      const [arrears, payments] = await Promise.all([listArrearsSettings(), getPaymentSettings()]);
      if (arrears.status !== 200 || payments.status !== 200) throw new Error("no disponibles");
      return { arrears: arrears.data, payments: payments.data };
    },
  });
  if (!query.data) {
    return (
      <QueryState
        isPending={query.isPending}
        isError={query.isError}
        isEmpty={false}
        emptyMessage=""
      />
    );
  }
  const general = query.data.arrears.settings.find((s) => !s.insurerId);
  return (
    <div className="space-y-4">
      <NumberForm
        label="Días de gracia para marcar mora"
        hint="Días tras el vencimiento de la cuota. Valor general de la agencia."
        initial={general?.graceDays ?? query.data.arrears.defaultGraceDays}
        onSave={(graceDays) => setArrearsSetting({ graceDays })}
      />
      <NumberForm
        label="Días de anticipación de un pago"
        hint="Cuánto antes del primer vencimiento se acepta la fecha de un pago."
        initial={query.data.payments.leadDays}
        onSave={(leadDays) => setPaymentSettings({ leadDays })}
      />
    </div>
  );
}
