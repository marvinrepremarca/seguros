// Feature flags en la UI (rules/feature-flags.md §3): FeatureGate oculta lo apagado y las rutas
// lo verifican en beforeLoad. Los valores vienen de /bff/features; el backend revalida siempre.
import { createContext, use } from "react";

import { featureKeys, type FeatureKey } from "./features.gen";

export type FeatureMap = Readonly<Partial<Record<string, boolean>>>;

export const FeaturesContext = createContext<FeatureMap>({});

/** Fail-closed: un flag ausente o desconocido está apagado. */
export function isFeatureEnabled(map: FeatureMap, key: FeatureKey): boolean {
  return map[key] === true;
}

export function useFeature(key: FeatureKey): boolean {
  return isFeatureEnabled(use(FeaturesContext), key);
}

export function isFeatureKey(value: string): value is FeatureKey {
  return featureKeys.some((k) => k === value);
}

export { features, type FeatureKey } from "./features.gen";
