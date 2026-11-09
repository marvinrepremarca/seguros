// Lo que compliance necesita de otros módulos: app/ lo compone e inyecta.
import type { ComponentType } from "react";

export interface ComplianceDeps {
  PartyPicker: ComponentType<{
    label: string;
    value: string;
    onChange: (partyId: string) => void;
    error?: string | undefined;
  }>;
}
