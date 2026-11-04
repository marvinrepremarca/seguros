// Composición: policies recibe de aquí lo que usa de catalog y parties (un módulo no importa a otro).
import { insurersQuery, linesQuery, productsQuery, riskSchemasQuery } from "@/modules/catalog";
import { PartyPicker } from "@/modules/parties";
import { agentsQuery, type PolicyDeps } from "@/modules/policies";

export const policyDeps: PolicyDeps = {
  insurers: insurersQuery(),
  lines: linesQuery(),
  products: productsQuery(),
  riskSchemas: riskSchemasQuery(),
  PartyPicker,
};

/** parties muestra y asigna asesores con la lista que publica policies. */
export const partyAgentsQuery = agentsQuery();
