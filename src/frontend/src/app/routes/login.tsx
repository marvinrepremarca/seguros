import { createFileRoute } from "@tanstack/react-router";
import { LogIn } from "lucide-react";
import { z } from "zod";

import { loginErrorMessage } from "@/modules/identity";
import { safeReturnTo } from "@/shared/lib/session";
import { Button } from "@/shared/ui/button";

const searchSchema = z.object({
  returnTo: z.string().max(512).optional(),
  error: z.string().max(120).optional(),
});

export const Route = createFileRoute("/login")({
  validateSearch: searchSchema,
  component: LoginPage,
});

function LoginPage() {
  const { returnTo, error } = Route.useSearch();
  const message = loginErrorMessage(error);
  // Navegación completa al BFF (no fetch): el login OIDC ocurre en Keycloak y la sesión vuelve
  // como cookie HttpOnly; el navegador nunca ve tokens.
  const href = `/bff/auth/login?returnTo=${encodeURIComponent(safeReturnTo(returnTo))}${
    error === "identity.login.mfa_required" ? "&reauth=true" : ""
  }`;
  return (
    <main className="grid min-h-dvh place-items-center p-4">
      <section
        aria-labelledby="login-title"
        className="w-full max-w-sm space-y-4 rounded-lg border p-6"
      >
        <h1 id="login-title" className="text-2xl font-semibold">
          Seguros ERP
        </h1>
        <p className="text-sm text-muted-foreground">Ingresa con la cuenta de tu agencia.</p>
        {message && (
          <p
            role="alert"
            className="rounded border border-destructive/40 p-3 text-sm text-destructive"
          >
            {message}
          </p>
        )}
        <Button asChild className="w-full">
          <a href={href}>
            <LogIn aria-hidden="true" />
            Iniciar sesión
          </a>
        </Button>
      </section>
    </main>
  );
}
