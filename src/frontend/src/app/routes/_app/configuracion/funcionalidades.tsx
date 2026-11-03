import { useQuery } from "@tanstack/react-query";
import { createFileRoute, notFound } from "@tanstack/react-router";

import { flagsListQuery, sourceLabel } from "@/modules/featuremgmt";
import { hasPermission, permissions } from "@/shared/lib/session";

export const Route = createFileRoute("/_app/configuracion/funcionalidades")({
  beforeLoad: ({ context }) => {
    // Solo UX: el endpoint exige featuremgmt.flag.read en el backend.
    if (!hasPermission(context.session, permissions.featureFlagRead)) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: FeaturesPage,
});

function FeaturesPage() {
  const { data, isPending, isError } = useQuery(flagsListQuery());
  return (
    <section aria-labelledby="features-title" className="space-y-4">
      <h1 id="features-title" className="text-2xl font-semibold">
        Funcionalidades
      </h1>
      <p className="text-sm text-muted-foreground">
        Módulos disponibles para tu agencia según el plan contratado. Los cambios de módulos los
        gestiona la plataforma.
      </p>
      {isPending && <p aria-busy="true">Cargando…</p>}
      {isError && <p role="alert">No se pudo cargar la configuración.</p>}
      {data?.length === 0 && <p>No hay funcionalidades registradas.</p>}
      {data !== undefined && data.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <caption className="sr-only">Estado de las funcionalidades</caption>
            <thead>
              <tr className="border-b">
                <th scope="col" className="py-2 pr-4">
                  Funcionalidad
                </th>
                <th scope="col" className="py-2 pr-4">
                  Estado
                </th>
                <th scope="col" className="py-2">
                  Origen
                </th>
              </tr>
            </thead>
            <tbody>
              {data.map((f) => (
                <tr key={f.key} className="border-b">
                  <td className="py-2 pr-4">{f.description}</td>
                  <td className="py-2 pr-4">{f.enabled ? "Activo" : "Inactivo"}</td>
                  <td className="py-2">{sourceLabel(f.source)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
