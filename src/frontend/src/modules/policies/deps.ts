// Lo que policies necesita de otros módulos. Un módulo no importa a otro (rules/frontend.md): app/ los
// compone e inyecta estas dependencias.
import type { ComponentType } from "react";

export interface Named {
  id: string;
  name: string;
  active: boolean;
}

export interface LineOption extends Named {
  riskSchemaKey: string;
}

export interface ProductOption extends Named {
  insurerId: string;
  lineOfBusinessId: string;
}

export interface RiskSchemaOption {
  key: string;
  label: string;
  schema: Readonly<Record<string, unknown>>;
}

interface ListQuery<T> {
  queryKey: readonly unknown[];
  queryFn: () => Promise<T[]>;
}

export interface PolicyDeps {
  insurers: ListQuery<Named>;
  lines: ListQuery<LineOption>;
  products: ListQuery<ProductOption>;
  riskSchemas: ListQuery<RiskSchemaOption>;
  PartyPicker: ComponentType<{
    label: string;
    value: string;
    onChange: (partyId: string) => void;
    error?: string | undefined;
  }>;
}
