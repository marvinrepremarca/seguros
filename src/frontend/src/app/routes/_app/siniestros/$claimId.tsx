import { createFileRoute, notFound } from "@tanstack/react-router";

import { ClaimDetailPage } from "@/modules/claims";
import { features, isFeatureEnabled } from "@/shared/lib/features";
import { FeatureGate } from "@/shared/lib/providers";
import { hasPermission, permissions } from "@/shared/lib/session";

export const Route = createFileRoute("/_app/siniestros/$claimId")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige flag y permiso en cada operación.
    if (
      !isFeatureEnabled(context.features, features.claims.module) ||
      !(
        hasPermission(context.session, permissions.claimRead) ||
        hasPermission(context.session, permissions.claimReadOwn)
      )
    ) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: Page,
});

function Page() {
  const { claimId } = Route.useParams();
  return (
    <FeatureGate flag={features.claims.module}>
      <ClaimDetailPage claimId={claimId} />
    </FeatureGate>
  );
}
