import { useMutation, useQuery } from "@tanstack/react-query";

import { problemMessage } from "@/shared/lib/problem";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, QueryState } from "@/shared/ui/field";

import { getQuotingSettings, setQuotingSettings } from "../api/generated/quoting";

/** Vigencia por defecto de una oferta (DECISIONS T3). */
export function QuotingSettings() {
  const query = useQuery({
    queryKey: ["quoting", "settings"],
    queryFn: async () => {
      const res = await getQuotingSettings();
      if (res.status !== 200) throw new Error("no disponibles");
      return res.data;
    },
  });
  const save = useMutation({
    mutationFn: async (offerValidDays: number) => {
      const res = await setQuotingSettings({ offerValidDays });
      if (res.status !== 204) throw new Error("no guardado");
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
  return (
    <form
      className="max-w-sm space-y-2"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate(Number(new FormData(e.currentTarget).get("days")));
      }}
    >
      <Field label="Días de vigencia de una oferta">
        {(p) => (
          <Input
            {...p}
            name="days"
            type="number"
            min={1}
            max={365}
            required
            defaultValue={query.data.offerValidDays}
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
