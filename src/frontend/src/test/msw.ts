// Servidor MSW para las pruebas: simula el BFF (rules/frontend.md §Pruebas).
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";

export const server = setupServer();

export interface FakeSession {
  displayName?: string;
  plan?: string;
  roles?: string[];
  permissions?: string[];
  flags?: Record<string, boolean>;
}

/** Simula una sesión autenticada con sus flags. */
export function signedIn(s: FakeSession = {}) {
  server.use(
    http.get("/bff/me", () =>
      HttpResponse.json({
        user: {
          id: "0192f3a4-1b2c-7d3e-8f40-123456789abc",
          displayName: s.displayName ?? "Ana Pérez",
        },
        tenant: { id: "0192f3a4-1b2c-7d3e-8f40-000000000001", plan: s.plan ?? "basico" },
        roles: s.roles ?? ["Manager"],
        permissions: s.permissions ?? ["featuremgmt.flag.read"],
      }),
    ),
    http.get("/bff/features", () => HttpResponse.json({ flags: s.flags ?? {} })),
  );
}

/** Simula que no hay sesión (el BFF responde 401 problem+json). */
export function signedOut() {
  const problem = {
    type: "authz.unauthenticated",
    title: "Se requiere autenticación",
    status: 401,
  };
  server.use(
    http.get("/bff/me", () => HttpResponse.json(problem, { status: 401 })),
    http.get("/bff/features", () => HttpResponse.json(problem, { status: 401 })),
  );
}
