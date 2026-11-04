#!/usr/bin/env bash
# Ejecuta un comando con la configuración de desarrollo de cmd/api (solo desarrollo): lee los
# secretos locales de deploy/.env y apunta a las dependencias de `make dev-up`. Los secretos se
# pasan por entorno, nunca por argumentos de línea de comandos.
# Uso: deploy/scripts/with-dev-env.sh go run ./cmd/api
set -euo pipefail

dir="$(cd "$(dirname "$0")/.." && pwd)"
[[ -f "$dir/.env" ]] || { echo "falta deploy/.env: ejecuta make dev-up" >&2; exit 1; }
get() { grep -E "^$1=" "$dir/.env" | cut -d= -f2-; }

# Dentro del Dev Container los puertos publicados del host se alcanzan por host.docker.internal.
host="${SEGUROS_DEV_HOST:-$([[ -f /.dockerenv ]] && echo host.docker.internal || echo localhost)}"

export SEGUROS_ENV=development
export SEGUROS_DB_URL="${SEGUROS_DB_URL:-$("$dir/scripts/dev-db-url.sh" app)}"
export SEGUROS_VALKEY_URL="redis://:$(get VALKEY_PASSWORD)@${host}:6379/0"
export SEGUROS_OIDC_ISSUER="http://localhost:8081/realms/seguros-dev"
export SEGUROS_OIDC_DISCOVERY_URL="http://${host}:8081/realms/seguros-dev"
export SEGUROS_OIDC_CLIENT_SECRET="$(get KC_BFF_CLIENT_SECRET)"
export SEGUROS_OIDC_DIRECTORY_CLIENT_SECRET="$(get KC_DIRECTORY_CLIENT_SECRET)"
export SEGUROS_PUBLIC_URL="${SEGUROS_PUBLIC_URL:-http://localhost:5173}"
# Los usuarios de prueba del realm no tienen OTP; en staging/producción la config lo impide.
export SEGUROS_AUTH_MFA_REQUIRED="${SEGUROS_AUTH_MFA_REQUIRED:-false}"
# Cifrado de campos con KEK local (solo desarrollo; staging/producción exigen OpenBao).
export SEGUROS_CRYPTO_LOCAL_KEKS="v1:$(get SEGUROS_CRYPTO_LOCAL_KEK)"
export SEGUROS_CRYPTO_INDEX_KEY="$(get SEGUROS_CRYPTO_INDEX_KEY)"
exec "$@"
