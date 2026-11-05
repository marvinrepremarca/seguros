import { createFileRoute, notFound } from "@tanstack/react-router";

import { LeadsPage } from "@/modules/crm";
import { features, isFeatureEnabled } from "@/shared/lib/features";
import { FeatureGate } from "@/shared/lib/providers";
import { hasPermission, permissions } from "@/shared/lib/session";

import { crmDeps } from "../../../sales-deps";

export const Route = createFileRoute("/_app/prospectos/")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige flag y permiso en cada operación.
    if (
      !isFeatureEnabled(context.features, features.crm.module) ||
      !(
        hasPermission(context.session, permissions.leadRead) ||
        hasPermission(context.session, permissions.leadReadOwn)
      )
    ) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: () => (
    <FeatureGate flag={features.crm.module}>
      <LeadsPage deps={crmDeps} />
    </FeatureGate>
  ),
});
