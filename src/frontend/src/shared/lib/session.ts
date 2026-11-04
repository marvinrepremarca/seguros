// Sesión del usuario autenticado (la provee app/ desde /bff/me). La UI usa roles y permisos solo
// para mostrar u ocultar: el backend autoriza cada operación (rules/frontend.md §Seguridad).
import { createContext, use } from "react";

export interface SessionData {
  user: { id: string; displayName: string };
  tenant: { id: string; plan: string };
  roles: readonly string[];
  permissions: readonly string[];
}

/** Permisos que la UI consulta (mismo catálogo que internal/platform/authz). */
export const permissions = {
  featureFlagRead: "featuremgmt.flag.read",
  featureFlagUpdate: "featuremgmt.flag.update",
  partyRead: "parties.party.read",
  partyCreate: "parties.party.create",
  partyUpdate: "parties.party.update",
  documentReveal: "parties.document.reveal",
  consentRead: "parties.consent.read",
  consentCreate: "parties.consent.create",
  catalogRead: "catalog.item.read",
  catalogManage: "catalog.item.manage",
  agreementRead: "catalog.commission-agreement.read",
  agreementManage: "catalog.commission-agreement.manage",
  policyRead: "policies.policy.read",
  policyReadOwn: "policies.policy.read-own",
  policyManage: "policies.policy.manage",
  policyCancel: "policies.policy.cancel",
  endorsementManage: "policies.endorsement.manage",
} as const;

export const SessionContext = createContext<SessionData | null>(null);

export function useSession(): SessionData {
  const s = use(SessionContext);
  if (!s) throw new Error("useSession fuera de SessionProvider");
  return s;
}

export function hasPermission(session: SessionData, permission: string): boolean {
  return session.permissions.includes(permission);
}

export function usePermission(permission: string): boolean {
  return hasPermission(useSession(), permission);
}

/** Valida que una ruta de retorno sea relativa a la propia origin (evita open redirect). */
export function safeReturnTo(value: unknown): string {
  if (
    typeof value !== "string" ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    value.includes("\\")
  ) {
    return "/";
  }
  return value;
}

/** Valida la URL de cierre de sesión que entrega el BFF antes de navegar a ella. */
export function safeExternalUrl(value: string): string | null {
  try {
    const u = new URL(value);
    return u.protocol === "https:" || u.protocol === "http:" ? u.toString() : null;
  } catch {
    return null;
  }
}
