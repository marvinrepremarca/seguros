import { createFileRoute, notFound } from "@tanstack/react-router";

import { DocumentsPage } from "@/modules/documents";
import { features, isFeatureEnabled } from "@/shared/lib/features";
import { FeatureGate } from "@/shared/lib/providers";
import { hasPermission, permissions } from "@/shared/lib/session";

import { documentsDeps } from "../../../sales-deps";

export const Route = createFileRoute("/_app/documentos/")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige flag y permiso en cada operación.
    if (
      !isFeatureEnabled(context.features, features.documents.module) ||
      !hasPermission(context.session, permissions.documentRead)
    ) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: () => (
    <FeatureGate flag={features.documents.module}>
      <DocumentsPage deps={documentsDeps} />
    </FeatureGate>
  ),
});
