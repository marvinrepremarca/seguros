import { createFileRoute, notFound } from "@tanstack/react-router";

import { ReceivablesPage } from "@/modules/billing";
import { features, isFeatureEnabled } from "@/shared/lib/features";
import { FeatureGate } from "@/shared/lib/providers";
import { hasPermission, permissions } from "@/shared/lib/session";

export const Route = createFileRoute("/_app/cartera/")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige flag y permiso en cada operación.
    if (
      !isFeatureEnabled(context.features, features.billing.module) ||
      !hasPermission(context.session, permissions.billingPlanRead)
    ) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: () => (
    <FeatureGate flag={features.billing.module}>
      <ReceivablesPage />
    </FeatureGate>
  ),
});
