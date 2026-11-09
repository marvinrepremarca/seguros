import { createFileRoute, notFound } from "@tanstack/react-router";

import { ReportingPage } from "@/modules/reporting";
import { features, isFeatureEnabled } from "@/shared/lib/features";
import { FeatureGate } from "@/shared/lib/providers";
import { hasPermission, permissions } from "@/shared/lib/session";

export const Route = createFileRoute("/_app/reportes/")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige flag y permiso en cada operación.
    if (
      !isFeatureEnabled(context.features, features.reporting.module) ||
      !hasPermission(context.session, permissions.reportingRead)
    ) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: () => (
    <FeatureGate flag={features.reporting.module}>
      <ReportingPage
        today={new Date().toLocaleDateString("sv-SE", { timeZone: "America/Bogota" })}
      />
    </FeatureGate>
  ),
});
