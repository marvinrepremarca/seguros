#!/usr/bin/env bash
# Prueba de humo E2E de punta a punta (ROADMAP 1.12), solo desarrollo/CI:
#   dependencias (make dev-up) → migraciones → tenant de prueba → build de la SPA → API → Playwright.
# Uso: deploy/scripts/e2e-full.sh [argumentos extra de playwright]
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$root"

[[ -f deploy/.env ]] || { echo "falta deploy/.env: ejecuta make dev-up" >&2; exit 1; }
get() { grep -E "^$1=" deploy/.env | cut -d= -f2-; }

make -s migrate-up >/dev/null
make -s dev-seed >/dev/null 2>&1 # idempotente: falla si el tenant ya existe

pids=()
cleanup() { for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT

# Dentro del Dev Container, el navegador de Playwright corre en el contenedor: Keycloak
# (localhost:8081, el issuer del realm) se alcanza con un túnel hacia el puerto publicado del host.
if [[ -f /.dockerenv ]] && ! (exec 3<>/dev/tcp/127.0.0.1/8081) 2>/dev/null; then
  socat TCP-LISTEN:8081,fork,reuseaddr,bind=127.0.0.1 TCP:host.docker.internal:8081 &
  pids+=($!)
fi

(cd src/backend && go build -o "${TMPDIR:-/tmp}/seguros-api-e2e" ./cmd/api)
frontend="${E2E_FRONTEND_DIR:-src/frontend}"
pnpm -C "$frontend" exec vite build >/dev/null

# La API sirve la SPA compilada con su CSP real (Vite no la aplica), en el puerto que el realm de
# Keycloak admite como redirect: todos los flujos, incluido el login, corren bajo esa CSP.
(cd src/backend && SEGUROS_HTTP_ADDR=:5173 SEGUROS_SPA_DIR="$root/$frontend/dist" \
  "$root/deploy/scripts/with-dev-env.sh" "${TMPDIR:-/tmp}/seguros-api-e2e") &
pids+=($!)
for _ in $(seq 1 60); do
  curl -sf http://localhost:5173/health/ready >/dev/null && break
  sleep 0.5
done
curl -sf http://localhost:5173/health/ready >/dev/null || { echo "la API no arrancó" >&2; exit 1; }

E2E_FULL=1 E2E_SPA_CSP=1 E2E_BASE_URL=http://localhost:5173 E2E_PASSWORD="$(get KC_TEST_USER_PASSWORD)" \
  pnpm -C "$frontend" exec playwright test "$@"
