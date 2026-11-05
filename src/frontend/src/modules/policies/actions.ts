// Qué acciones ofrece la UI según estado y permisos. Solo es UX: la máquina de estados y los permisos
// se validan en el backend.
import type { PolicyStatus } from "./api/generated/policies";

export type PolicyAction =
  | "edit"
  | "accept"
  | "issue"
  | "reject"
  | "cancel"
  | "endorse"
  | "reassign"
  | "renew"
  | "reinstate";

const BY_STATUS: Readonly<Record<PolicyStatus, readonly PolicyAction[]>> = {
  in_quotation: ["edit", "accept", "reassign"],
  in_process: ["edit", "issue", "reject", "reassign"],
  active: ["endorse", "cancel", "reassign", "renew"],
  in_arrears: ["endorse", "cancel", "reassign", "renew", "reinstate"],
  cancelled: [],
  expired: ["reassign", "renew"],
  renewed: [],
  not_renewed: [],
  rejected: [],
};

interface ActionPermissions {
  manage: boolean;
  cancel: boolean;
  endorse: boolean;
  assign: boolean;
}

export function availableActions(status: PolicyStatus, can: ActionPermissions): PolicyAction[] {
  return BY_STATUS[status].filter((a) => {
    switch (a) {
      case "cancel":
        return can.cancel;
      case "endorse":
        return can.endorse;
      case "reassign":
        return can.assign;
      case "edit":
      case "accept":
      case "issue":
      case "reject":
      case "renew":
      case "reinstate":
        return can.manage;
    }
  });
}
