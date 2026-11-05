import { createFileRoute, notFound } from "@tanstack/react-router";

import { QuoteDetailPage } from "@/modules/quoting";
import { features, isFeatureEnabled } from "@/shared/lib/features";
import { FeatureGate } from "@/shared/lib/providers";
import { hasPermission, permissions } from "@/shared/lib/session";

import { quotingDeps } from "../../../sales-deps";

export const Route = createFileRoute("/_app/cotizaciones/$requestId")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige flag y permiso en cada operación.
    if (
      !isFeatureEnabled(context.features, features.quoting.module) ||
      !(
        hasPermission(context.session, permissions.quoteRead) ||
        hasPermission(context.session, permissions.quoteReadOwn)
      )
    ) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: Page,
});

function Page() {
  const { requestId } = Route.useParams();
  return (
    <FeatureGate flag={features.quoting.module}>
      <QuoteDetailPage requestId={requestId} deps={quotingDeps} />
    </FeatureGate>
  );
}
