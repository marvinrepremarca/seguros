import { useSuspenseQuery } from "@tanstack/react-query";
import { Outlet, createFileRoute, redirect } from "@tanstack/react-router";

import { featuresQuery } from "@/modules/featuremgmt";
import { sessionQuery } from "@/modules/identity";
import { ApiError } from "@/shared/lib/api-client";
import { FeaturesProvider, SessionProvider } from "@/shared/lib/providers";

import { AppShell } from "../AppShell";

// Layout de todas las rutas autenticadas: sin sesión redirige a /login conservando el destino.
export const Route = createFileRoute("/_app")({
  beforeLoad: async ({ context, location }) => {
    try {
      const [session, features] = await Promise.all([
        context.queryClient.query(sessionQuery()),
        context.queryClient.query(featuresQuery()),
      ]);
      return { session, features };
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
        throw redirect({ to: "/login", search: { returnTo: location.href } });
      }
      throw err;
    }
  },
  component: AuthenticatedLayout,
  errorComponent: LoadError,
});

function AuthenticatedLayout() {
  const { data: session } = useSuspenseQuery(sessionQuery());
  const { data: features } = useSuspenseQuery(featuresQuery());
  return (
    <SessionProvider value={session}>
      <FeaturesProvider value={features}>
        <AppShell session={session} features={features}>
          <Outlet />
        </AppShell>
      </FeaturesProvider>
    </SessionProvider>
  );
}

function LoadError() {
  return (
    <main className="grid min-h-dvh place-items-center p-4">
      <p role="alert">No se pudo cargar la aplicación. Recarga la página o inténtalo más tarde.</p>
    </main>
  );
}
