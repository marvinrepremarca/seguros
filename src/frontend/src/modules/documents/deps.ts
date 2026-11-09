// Lo que documents necesita de otros módulos: app/ lo compone e inyecta.
import type { ComponentType } from "react";

interface OptionsQuery {
  queryKey: readonly unknown[];
  queryFn: () => Promise<{ id: string; label: string }[]>;
}

export interface DocumentsDeps {
  PartyPicker: ComponentType<{
    label: string;
    value: string;
    onChange: (partyId: string) => void;
    error?: string | undefined;
  }>;
  policies: OptionsQuery;
  claims: OptionsQuery;
}
