// Composición: policies recibe de aquí lo que usa de catalog y parties (un módulo no importa a otro).
import { insurersQuery, linesQuery, productsQuery, riskSchemasQuery } from "@/modules/catalog";
import { PartyPicker } from "@/modules/parties";
import type { PolicyDeps } from "@/modules/policies";

export const policyDeps: PolicyDeps = {
  insurers: insurersQuery(),
  lines: linesQuery(),
  products: productsQuery(),
  riskSchemas: riskSchemasQuery(),
  PartyPicker,
};
