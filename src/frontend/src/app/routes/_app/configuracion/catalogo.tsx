import { createFileRoute, notFound } from "@tanstack/react-router";

import { CatalogPage } from "@/modules/catalog";
import { hasPermission, permissions } from "@/shared/lib/session";

export const Route = createFileRoute("/_app/configuracion/catalogo")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige el permiso en cada operación.
    if (!hasPermission(context.session, permissions.catalogRead)) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: CatalogPage,
});
