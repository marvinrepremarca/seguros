import { createFileRoute, notFound } from "@tanstack/react-router";

import { QuoteRequestsPage } from "@/modules/quoting";
import { features, isFeatureEnabled } from "@/shared/lib/features";
import { FeatureGate } from "@/shared/lib/providers";
import { hasPermission, permissions } from "@/shared/lib/session";

import { quotingDeps } from "../../../sales-deps";

const UUID = /^[0-9a-f-]{36}$/i;
const asId = (v: unknown): string | undefined =>
  typeof v === "string" && UUID.test(v) ? v : undefined;

export const Route = createFileRoute("/_app/cotizaciones/")({
  validateSearch: (s: Record<string, unknown>): { leadId?: string; partyId?: string } => {
    const leadId = asId(s["leadId"]);
    const partyId = asId(s["partyId"]);
    return { ...(leadId ? { leadId } : {}), ...(partyId ? { partyId } : {}) };
  },
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
  const { leadId, partyId } = Route.useSearch();
  return (
    <FeatureGate flag={features.quoting.module}>
      <QuoteRequestsPage deps={quotingDeps} leadId={leadId} partyId={partyId} />
    </FeatureGate>
  );
}
