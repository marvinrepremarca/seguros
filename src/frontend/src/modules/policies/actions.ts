// Qué acciones ofrece la UI según estado y permisos. Solo es UX: la máquina de estados y los permisos
// se validan en el backend.
import type { PolicyStatus } from "./api/generated/policies";

export type PolicyAction = "edit" | "accept" | "issue" | "reject" | "cancel" | "endorse";

const BY_STATUS: Readonly<Record<PolicyStatus, readonly PolicyAction[]>> = {
  in_quotation: ["edit", "accept"],
  in_process: ["edit", "issue", "reject"],
  active: ["endorse", "cancel"],
  in_arrears: ["endorse", "cancel"],
  cancelled: [],
  expired: [],
  renewed: [],
  not_renewed: [],
  rejected: [],
};

interface ActionPermissions {
  manage: boolean;
  cancel: boolean;
  endorse: boolean;
}

export function availableActions(status: PolicyStatus, can: ActionPermissions): PolicyAction[] {
  return BY_STATUS[status].filter((a) => {
    switch (a) {
      case "cancel":
        return can.cancel;
      case "endorse":
        return can.endorse;
      case "edit":
      case "accept":
      case "issue":
      case "reject":
        return can.manage;
    }
  });
}
