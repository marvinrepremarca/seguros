import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  component: HomePage,
});

function HomePage() {
  return (
    <section aria-labelledby="home-title" className="space-y-2">
      <h1 id="home-title" className="text-2xl font-semibold">
        Seguros ERP
      </h1>
      <p className="text-muted-foreground">Plataforma en construcción.</p>
    </section>
  );
}
