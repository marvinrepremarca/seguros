import { createFileRoute, notFound } from "@tanstack/react-router";

import { CompliancePage } from "@/modules/compliance";
import { features, isFeatureEnabled } from "@/shared/lib/features";
import { FeatureGate } from "@/shared/lib/providers";
import { hasPermission, permissions } from "@/shared/lib/session";

import { complianceDeps } from "../../../sales-deps";

export const Route = createFileRoute("/_app/cumplimiento/")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige flag y permiso en cada operación.
    if (
      !isFeatureEnabled(context.features, features.compliance.module) ||
      !hasPermission(context.session, permissions.kycRead)
    ) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: () => (
    <FeatureGate flag={features.compliance.module}>
      <CompliancePage deps={complianceDeps} />
    </FeatureGate>
  ),
});
