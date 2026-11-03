// Superficie pública del módulo identity en el frontend (consultas de sesión y logout sobre el
// cliente generado por orval).
import { queryOptions } from "@tanstack/react-query";

import type { SessionData } from "@/shared/lib/session";

import { getSession, logout } from "./api/generated/identity";

export const sessionQueryKey = ["identity", "session"] as const;

export function sessionQuery() {
  return queryOptions({
    queryKey: sessionQueryKey,
    queryFn: async (): Promise<SessionData> => {
      const res = await getSession();
      if (res.status !== 200) throw new Error("sesión inválida");
      return res.data;
    },
    staleTime: 60_000,
    retry: false,
  });
}

/** Cierra la sesión en el BFF y devuelve la URL de cierre del proveedor de identidad. */
export async function logoutSession(): Promise<string> {
  const res = await logout();
  if (res.status !== 200) throw new Error("no se pudo cerrar la sesión");
  return res.data.logoutUrl;
}

/** Mensajes en español para los códigos con los que el BFF devuelve a /login. */
export function loginErrorMessage(code: string | undefined): string | null {
  switch (code) {
    case undefined:
    case "":
      return null;
    case "identity.login.mfa_required":
      return "Tu rol exige verificación en dos pasos. Configura un segundo factor en tu cuenta e inténtalo de nuevo.";
    case "identity.login.tenant_unavailable":
      return "Tu agencia no tiene acceso activo. Comunícate con el administrador de la plataforma.";
    case "identity.login.organization_required":
      return "Tu usuario no está asociado a una agencia.";
    case "identity.login.no_roles":
      return "Tu usuario no tiene roles asignados. Pide acceso al administrador de tu agencia.";
    default:
      return "No se pudo completar el inicio de sesión. Inténtalo de nuevo.";
  }
}
