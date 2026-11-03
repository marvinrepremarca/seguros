// Superficie pública del módulo featuremgmt en el frontend.
import { queryOptions } from "@tanstack/react-query";

import type { FeatureMap } from "@/shared/lib/features";

import { getFeatures, listFlags, type Flag } from "./api/generated/featuremgmt";

export const featuresQueryKey = ["featuremgmt", "features"] as const;

export function featuresQuery() {
  return queryOptions({
    queryKey: featuresQueryKey,
    queryFn: async (): Promise<FeatureMap> => {
      const res = await getFeatures();
      if (res.status !== 200) throw new Error("flags no disponibles");
      return res.data.flags;
    },
    staleTime: 30_000,
    retry: false,
  });
}

export function flagsListQuery() {
  return queryOptions({
    queryKey: ["featuremgmt", "flags"] as const,
    queryFn: async (): Promise<Flag[]> => {
      const res = await listFlags();
      if (res.status !== 200) throw new Error("no se pudo cargar la configuración");
      return res.data.items;
    },
  });
}

export type { Flag };

/** Etiqueta en español del origen del valor de un flag. */
export function sourceLabel(source: Flag["source"]): string {
  switch (source) {
    case "plan":
      return "Incluido en el plan";
    case "override":
      return "Ajuste de la plataforma";
    case "default":
      return "Valor por defecto";
    case "kill-switch":
      return "Desactivado temporalmente";
    case "dependency":
      return "Requiere otro módulo";
    case "global":
      return "Configuración global";
    case "error":
      return "No disponible";
  }
}
