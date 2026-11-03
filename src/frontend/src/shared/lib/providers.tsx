// Proveedores de contexto de sesión y flags (los alimenta app/ con los datos del BFF).
import type { ReactNode } from "react";

import { FeaturesContext, useFeature, type FeatureKey, type FeatureMap } from "./features";
import { SessionContext, type SessionData } from "./session";

export function SessionProvider({ value, children }: { value: SessionData; children: ReactNode }) {
  return <SessionContext value={value}>{children}</SessionContext>;
}

export function FeaturesProvider({ value, children }: { value: FeatureMap; children: ReactNode }) {
  return <FeaturesContext value={value}>{children}</FeaturesContext>;
}

/** Muestra children solo si el flag está encendido (fail-closed). No es autorización. */
export function FeatureGate({
  flag,
  children,
  fallback = null,
}: {
  flag: FeatureKey;
  children: ReactNode;
  fallback?: ReactNode;
}) {
  return useFeature(flag) ? children : fallback;
}
