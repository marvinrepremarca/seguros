// Lo que crm necesita de otros módulos: app/ lo compone e inyecta.
import type { ComponentType } from "react";

export interface CrmDeps {
  PartyPicker: ComponentType<{
    label: string;
    value: string;
    onChange: (partyId: string) => void;
    error?: string | undefined;
  }>;
  agents: {
    queryKey: readonly unknown[];
    queryFn: () => Promise<{ id: string; displayName: string; active: boolean }[]>;
  };
}
