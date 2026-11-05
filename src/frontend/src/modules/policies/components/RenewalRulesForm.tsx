import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { problemMessage } from "@/shared/lib/problem";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, QueryState } from "@/shared/ui/field";

import { getRenewalRules, setRenewalRules, type RenewalRules } from "../api/generated/policies";

function Form({ initial }: { initial: RenewalRules }) {
  const [rules, setRules] = useState(initial);
  const save = useMutation({
    mutationFn: async () => {
      const res = await setRenewalRules(rules);
      if (res.status !== 204) throw new Error("no guardado");
    },
  });
  return (
    <form
      className="space-y-2 text-sm"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <label className="flex items-center gap-2">
        <input
          type="checkbox"
          checked={rules.keepHolder}
          onChange={(e) => {
            setRules({ ...rules, keepHolder: e.target.checked });
          }}
        />
        Una renovación conserva el tomador
      </label>
      <label className="flex items-center gap-2">
        <input
          type="checkbox"
          checked={rules.keepLine}
          onChange={(e) => {
            setRules({ ...rules, keepLine: e.target.checked });
          }}
        />
        Una renovación conserva el ramo
      </label>
      <ErrorMessage message={problemMessage(save.error)} />
      {save.isSuccess && <p role="status">Guardado.</p>}
      <Button type="submit" variant="outline" disabled={save.isPending}>
        Guardar
      </Button>
    </form>
  );
}

/** Qué debe conservar una renovación (DECISIONS N25). */
export function RenewalRulesForm() {
  const query = useQuery({
    queryKey: ["policies", "renewal-rules"],
    queryFn: async () => {
      const res = await getRenewalRules();
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
  return <Form initial={query.data} />;
}
