import { createFileRoute, notFound } from "@tanstack/react-router";

import { PartyDetailPage } from "@/modules/parties";
import { hasPermission, permissions } from "@/shared/lib/session";

export const Route = createFileRoute("/_app/clientes/$partyId")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige el permiso en cada operación.
    if (!hasPermission(context.session, permissions.partyRead)) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: Page,
});

function Page() {
  const { partyId } = Route.useParams();
  return <PartyDetailPage key={partyId} partyId={partyId} />;
}
