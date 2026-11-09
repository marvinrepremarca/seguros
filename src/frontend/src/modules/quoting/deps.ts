// Lo que quoting necesita de otros módulos: app/ lo compone e inyecta.
import type { ComponentType } from "react";

interface ListQuery<T> {
  queryKey: readonly unknown[];
  queryFn: () => Promise<T[]>;
}

export interface RiskFieldOption {
  name: string;
  label: string;
  type: "string" | "integer";
  required: boolean;
}

export interface QuotingDeps {
  insurers: ListQuery<{ id: string; name: string; active: boolean }>;
  lines: ListQuery<{ id: string; name: string; active: boolean; riskSchemaKey: string }>;
  riskSchemas: ListQuery<{ key: string; label: string; schema: Readonly<Record<string, unknown>> }>;
  riskFields: (schema: Readonly<Record<string, unknown>> | undefined) => RiskFieldOption[];
  toRiskData: (
    fields: readonly RiskFieldOption[],
    values: Readonly<Record<string, string | undefined>>,
  ) => Record<string, unknown>;
  RiskFields: ComponentType<{
    fields: readonly RiskFieldOption[];
    values: Readonly<Record<string, string>>;
    onChange: (name: string, value: string) => void;
  }>;
  PartyPicker: ComponentType<{
    label: string;
    value: string;
    onChange: (partyId: string) => void;
    error?: string | undefined;
  }>;
}
