import { createFileRoute, notFound } from "@tanstack/react-router";

import { PortfolioTransferPage } from "@/modules/policies";
import { hasPermission, permissions } from "@/shared/lib/session";

export const Route = createFileRoute("/_app/configuracion/traspaso-cartera")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige el permiso en cada operación.
    if (!hasPermission(context.session, permissions.policyAssign)) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: PortfolioTransferPage,
});
