import { createFileRoute, notFound } from "@tanstack/react-router";

import { CommissionsPage } from "@/modules/commissions";
import { features, isFeatureEnabled } from "@/shared/lib/features";
import { FeatureGate } from "@/shared/lib/providers";
import { hasPermission, permissions } from "@/shared/lib/session";

export const Route = createFileRoute("/_app/comisiones/")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige flag y permiso en cada operación.
    if (
      !isFeatureEnabled(context.features, features.commissions.module) ||
      !hasPermission(context.session, permissions.commissionRead)
    ) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: () => (
    <FeatureGate flag={features.commissions.module}>
      <CommissionsPage />
    </FeatureGate>
  ),
});
