// Cliente HTTP único para el código generado por orval. Habla solo con el BFF (misma origin):
// cookies httpOnly, sin tokens en JS, con header anti-CSRF en métodos que modifican.
const CSRF_HEADER = "X-CSRF-Token";
const CSRF_COOKIE = "__Host-csrf";
const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

function readCookie(name: string): string | undefined {
  const prefix = `${name}=`;
  return document.cookie
    .split("; ")
    .find((c) => c.startsWith(prefix))
    ?.slice(prefix.length);
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly problem: unknown,
  ) {
    super(`HTTP ${String(status)}`);
    this.name = "ApiError";
  }
}

export async function apiClient<T>(url: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json, application/problem+json");
  if (!SAFE_METHODS.has(method)) {
    const token = readCookie(CSRF_COOKIE);
    if (token) headers.set(CSRF_HEADER, token);
  }

  // Único punto autorizado para usar fetch (la regla no-restricted-globals aplica al resto).
  // eslint-disable-next-line no-restricted-globals
  const res = await fetch(url, { ...init, method, headers, credentials: "same-origin" });
  const body: unknown = res.status === 204 ? undefined : await res.json().catch(() => undefined);
  if (!res.ok) throw new ApiError(res.status, body);
  // Límite de confianza: el contrato OpenAPI (y los esquemas Zod generados) definen T.
  // eslint-disable-next-line @typescript-eslint/consistent-type-assertions
  return { data: body, status: res.status, headers: res.headers } as T;
}
