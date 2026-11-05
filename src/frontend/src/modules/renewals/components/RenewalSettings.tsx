import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { problemMessage } from "@/shared/lib/problem";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState } from "@/shared/ui/field";

import { getRenewalSettings, setRenewalSettings } from "../api/generated/renewals";

function Form({ initial }: { initial: number[] }) {
  const [text, setText] = useState(initial.join(", "));
  const save = useMutation({
    mutationFn: async () => {
      const alertDays = text
        .split(",")
        .map((x) => x.trim())
        .filter((x) => x !== "")
        .map(Number);
      const res = await setRenewalSettings({ alertDays });
      if (res.status !== 204) throw new Error("no guardado");
    },
  });
  return (
    <form
      className="max-w-sm space-y-2"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <Field
        label="Días de alerta de renovación"
        hint="Antes del fin de vigencia, separados por coma."
      >
        {(p) => (
          <Input
            {...p}
            required
            value={text}
            onChange={(e) => {
              setText(e.target.value);
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

/** Umbrales de alerta de la agencia (DECISIONS T2). */
export function RenewalSettings() {
  const query = useQuery({
    queryKey: ["renewals", "settings"],
    queryFn: async () => {
      const res = await getRenewalSettings();
      if (res.status !== 200) throw new Error("no disponibles");
      return res.data;
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
  return <Form initial={query.data.alertDays} />;
}
