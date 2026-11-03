import { createFileRoute, notFound } from "@tanstack/react-router";

import { isFeatureEnabled, isFeatureKey } from "@/shared/lib/features";
import { FeatureGate } from "@/shared/lib/providers";

import { MODULE_LABELS } from "../../../navigation";

// Página de un módulo apagable. beforeLoad responde 404 si el flag del módulo está apagado (el
// código del módulo no se descarga: cada ruta es un chunk lazy) y FeatureGate lo vuelve a exigir.
export const Route = createFileRoute("/_app/modulos/$modulo")({
  beforeLoad: ({ context, params }) => {
    const key = `module.${params.modulo}`;
    if (!isFeatureKey(key) || !isFeatureEnabled(context.features, key)) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
    return { flag: key };
  },
  component: ModulePage,
});

function ModulePage() {
  const { flag } = Route.useRouteContext();
  const { modulo } = Route.useParams();
  return (
    <FeatureGate flag={flag}>
      <section aria-labelledby="module-title" className="space-y-2">
        <h1 id="module-title" className="text-2xl font-semibold">
          {MODULE_LABELS[modulo] ?? modulo}
        </h1>
        <p className="text-muted-foreground">Módulo en construcción.</p>
      </section>
    </FeatureGate>
  );
}
