// Código generado por `make generate` desde src/backend/internal/platform/flags/registry.yaml. NO EDITAR.
// El backend es la autoridad: estas claves solo deciden qué mostrar (rules/feature-flags.md).

export const features = {
  billing: {
    module: "module.billing",
  },
  claims: {
    module: "module.claims",
  },
  commissions: {
    module: "module.commissions",
  },
  compliance: {
    module: "module.compliance",
  },
  crm: {
    module: "module.crm",
  },
  documents: {
    module: "module.documents",
  },
  integrations: {
    module: "module.integrations",
  },
  notifications: {
    module: "module.notifications",
  },
  quoting: {
    module: "module.quoting",
  },
  renewals: {
    module: "module.renewals",
  },
  reporting: {
    module: "module.reporting",
  },
  ops: {
    integrations: {
      simulator: "ops.integrations.simulator",
    },
  },
} as const;

export const featureKeys = [
  "module.billing",
  "module.claims",
  "module.commissions",
  "module.compliance",
  "module.crm",
  "module.documents",
  "module.integrations",
  "module.notifications",
  "module.quoting",
  "module.renewals",
  "module.reporting",
  "ops.integrations.simulator",
] as const;

export type FeatureKey = (typeof featureKeys)[number];
