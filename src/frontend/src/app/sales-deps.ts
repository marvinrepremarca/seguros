// Composición: crm, quoting y claims reciben de aquí lo que usan de otros módulos.
import { insurersQuery, linesQuery, riskSchemasQuery } from "@/modules/catalog";
import { claimOptionsQuery, type ClaimsDeps } from "@/modules/claims";
import type { ComplianceDeps } from "@/modules/compliance";
import type { DocumentsDeps } from "@/modules/documents";
import type { CrmDeps } from "@/modules/crm";
import { PartyPicker } from "@/modules/parties";
import {
  agentsQuery,
  policyOptionsQuery,
  RiskFields,
  riskFields,
  toRiskData,
} from "@/modules/policies";
import type { QuotingDeps } from "@/modules/quoting";

export const crmDeps: CrmDeps = { PartyPicker, agents: agentsQuery() };

export const quotingDeps: QuotingDeps = {
  insurers: insurersQuery(),
  lines: linesQuery(),
  riskSchemas: riskSchemasQuery(),
  riskFields,
  toRiskData,
  RiskFields,
  PartyPicker,
};

export const claimsDeps: ClaimsDeps = { policies: policyOptionsQuery() };

export const complianceDeps: ComplianceDeps = { PartyPicker };

export const documentsDeps: DocumentsDeps = {
  PartyPicker,
  policies: policyOptionsQuery(),
  claims: claimOptionsQuery(),
};
