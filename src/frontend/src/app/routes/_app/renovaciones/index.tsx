import { createFileRoute, notFound } from "@tanstack/react-router";

import { RenewalsPage } from "@/modules/renewals";
import { features, isFeatureEnabled } from "@/shared/lib/features";
import { FeatureGate } from "@/shared/lib/providers";
import { hasPermission, permissions } from "@/shared/lib/session";

export const Route = createFileRoute("/_app/renovaciones/")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige flag y permiso en cada operación.
    if (
      !isFeatureEnabled(context.features, features.renewals.module) ||
      !hasPermission(context.session, permissions.renewalRead)
    ) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: () => (
    <FeatureGate flag={features.renewals.module}>
      <RenewalsPage />
    </FeatureGate>
  ),
});
