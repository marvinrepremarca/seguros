import { createFileRoute, notFound } from "@tanstack/react-router";

import { PartiesPage } from "@/modules/parties";
import { hasPermission, permissions } from "@/shared/lib/session";

export const Route = createFileRoute("/_app/clientes/")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige el permiso en cada operación.
    if (!hasPermission(context.session, permissions.partyRead)) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: PartiesPage,
});
