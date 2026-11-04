import { createFileRoute, notFound } from "@tanstack/react-router";

import { PolicyFormPage } from "@/modules/policies";
import { hasPermission, permissions } from "@/shared/lib/session";

import { policyDeps } from "../../../../policy-deps";

export const Route = createFileRoute("/_app/polizas/$policyId/editar")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige el permiso en cada operación.
    if (!hasPermission(context.session, permissions.policyManage)) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: Page,
});

function Page() {
  const { policyId } = Route.useParams();
  return <PolicyFormPage key={policyId} policyId={policyId} deps={policyDeps} />;
}
