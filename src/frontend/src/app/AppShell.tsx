import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { LogOut } from "lucide-react";
import type { ReactNode } from "react";

import { logoutSession } from "@/modules/identity";
import { isFeatureEnabled, type FeatureMap } from "@/shared/lib/features";
import { hasPermission, safeExternalUrl, type SessionData } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";

import { NAV_SECTIONS, visibleSections } from "./navigation";

const PLAN_LABELS: Readonly<Record<string, string>> = {
  basico: "Plan Básico",
  profesional: "Plan Profesional",
  empresarial: "Plan Empresarial",
};

interface AppShellProps {
  session: SessionData;
  features: FeatureMap;
  children: ReactNode;
}

export function AppShell({ session, features, children }: AppShellProps) {
  const queryClient = useQueryClient();
  const logout = useMutation({
    mutationFn: logoutSession,
    onSuccess: (logoutUrl) => {
      queryClient.clear();
      window.location.assign(safeExternalUrl(logoutUrl) ?? "/login");
    },
  });
  const sections = visibleSections(
    NAV_SECTIONS,
    (k) => isFeatureEnabled(features, k),
    (p) => hasPermission(session, p),
  );

  return (
    <div className="min-h-dvh md:grid md:grid-cols-[16rem_1fr]">
      <a
        href="#contenido"
        className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:rounded focus:bg-background focus:p-2"
      >
        Saltar al contenido
      </a>
      <aside className="border-b bg-secondary md:border-r md:border-b-0">
        <div className="p-4">
          <p className="text-lg font-semibold">Seguros ERP</p>
          <p className="text-sm text-muted-foreground">
            {PLAN_LABELS[session.tenant.plan] ?? session.tenant.plan}
          </p>
        </div>
        <nav aria-label="Menú principal" className="px-2 pb-4">
          {sections.map((s) => (
            <div key={s.title} className="mb-3">
              <h2 className="px-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
                {s.title}
              </h2>
              <ul className="mt-1">
                {s.items.map((i) => (
                  <li key={i.label}>
                    <Link
                      {...i.link}
                      className="block rounded px-2 py-1.5 text-sm hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                      activeProps={{ className: "bg-accent font-medium", "aria-current": "page" }}
                      activeOptions={{ exact: i.link.to === "/" }}
                    >
                      {i.label}
                    </Link>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </nav>
      </aside>
      <div className="flex min-w-0 flex-col">
        <header className="flex items-center justify-end gap-3 border-b px-4 py-2">
          <span className="text-sm">{session.user.displayName}</span>
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              logout.mutate();
            }}
            disabled={logout.isPending}
          >
            <LogOut aria-hidden="true" />
            Cerrar sesión
          </Button>
        </header>
        <main id="contenido" className="mx-auto w-full max-w-5xl flex-1 p-4 sm:p-6">
          {logout.isError && (
            <p role="alert" className="mb-4 text-sm text-destructive">
              No se pudo cerrar la sesión. Inténtalo de nuevo.
            </p>
          )}
          {children}
        </main>
      </div>
    </div>
  );
}
