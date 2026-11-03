import { createFileRoute } from "@tanstack/react-router";

import { useSession } from "@/shared/lib/session";

export const Route = createFileRoute("/_app/")({
  component: HomePage,
});

function HomePage() {
  const session = useSession();
  return (
    <section aria-labelledby="home-title" className="space-y-2">
      <h1 id="home-title" className="text-2xl font-semibold">
        Bienvenido, {session.user.displayName}
      </h1>
      <p className="text-muted-foreground">Selecciona un módulo en el menú.</p>
    </section>
  );
}
