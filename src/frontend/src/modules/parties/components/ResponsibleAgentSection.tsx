import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { problemMessage } from "@/shared/lib/problem";
import { Button } from "@/shared/ui/button";
import { ErrorMessage, Field, Input, Select } from "@/shared/ui/field";

import { assignPartyResponsible, type Party } from "../api/generated/parties";
import type { AgentsQuery } from "../deps";

/** Asesor responsable del cliente y su traspaso (solo Manager y Operations). */
export function ResponsibleAgentSection({
  party,
  agentsQuery,
}: {
  party: Party;
  agentsQuery: AgentsQuery;
}) {
  const queryClient = useQueryClient();
  const agents = useQuery(agentsQuery);
  const [agentId, setAgentId] = useState("");
  const [reason, setReason] = useState("");
  const assign = useMutation({
    mutationFn: () => assignPartyResponsible(party.id, { agentId, reason, version: party.version }),
    onSuccess: async () => {
      setAgentId("");
      setReason("");
      await queryClient.invalidateQueries({ queryKey: ["parties"] });
    },
  });
  const current = agents.data?.find((a) => a.id === party.responsibleAgentId);
  return (
    <form
      className="space-y-3"
      onSubmit={(e) => {
        e.preventDefault();
        assign.mutate();
      }}
    >
      <h2 className="text-lg font-semibold">Asesor responsable</h2>
      <p>
        {party.responsibleAgentId
          ? (current?.displayName ?? "Asesor no disponible")
          : "Sin asesor responsable"}
      </p>
      <ErrorMessage message={problemMessage(assign.error)} />
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Asesor nuevo">
          {(p) => (
            <Select
              {...p}
              required
              value={agentId}
              onChange={(e) => {
                setAgentId(e.target.value);
              }}
            >
              <option value="">Selecciona…</option>
              {agents.data
                ?.filter((a) => a.active && a.id !== party.responsibleAgentId)
                .map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.displayName}
                  </option>
                ))}
            </Select>
          )}
        </Field>
        <Field label="Motivo del cambio">
          {(p) => (
            <Input
              {...p}
              required
              minLength={3}
              maxLength={200}
              value={reason}
              onChange={(e) => {
                setReason(e.target.value);
              }}
            />
          )}
        </Field>
      </div>
      <Button type="submit" variant="outline" disabled={assign.isPending}>
        Cambiar asesor responsable
      </Button>
    </form>
  );
}
