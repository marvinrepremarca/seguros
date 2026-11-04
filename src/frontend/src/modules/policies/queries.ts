// Consultas de pólizas que comparten varias pantallas.
import { listAgents } from "./api/generated/policies";

async function fetchAgents() {
  const res = await listAgents();
  if (res.status !== 200) throw new Error("asesores no disponibles");
  return res.data.items;
}

export const agentsQuery = () => ({ queryKey: ["policies", "agents"], queryFn: fetchAgents });
