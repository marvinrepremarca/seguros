// Consultas de pólizas que comparten varias pantallas.
import { listAgents, listPolicies } from "./api/generated/policies";

async function fetchAgents() {
  const res = await listAgents();
  if (res.status !== 200) throw new Error("asesores no disponibles");
  return res.data.items;
}

export const agentsQuery = () => ({ queryKey: ["policies", "agents"], queryFn: fetchAgents });

async function fetchPolicyOptions() {
  const res = await listPolicies({ limit: 200 });
  if (res.status !== 200) throw new Error("pólizas no disponibles");
  return res.data.items.map((p) => ({
    id: p.id,
    label: `${p.policyNumber ?? "Sin número"} · ${p.holderName}`,
  }));
}

/** Pólizas que el usuario puede ver, para elegir una en otro módulo. */
export const policyOptionsQuery = () => ({
  queryKey: ["policies", "options"],
  queryFn: fetchPolicyOptions,
});
