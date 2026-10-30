#!/usr/bin/env bash
# Imprime la URL de conexión del PostgreSQL de desarrollo para un rol (app | migrator), leyendo la
# contraseña de deploy/.env. Solo desarrollo; úsala dentro de una sustitución de comando para no
# mostrar la contraseña: SEGUROS_DB_URL="$(deploy/scripts/dev-db-url.sh app)" ...
set -euo pipefail

dir="$(cd "$(dirname "$0")/.." && pwd)"
role="${1:-}"
case "$role" in
  app)      user=seguros_app;      key=SEGUROS_APP_PASSWORD ;;
  migrator) user=seguros_migrator; key=SEGUROS_MIGRATOR_PASSWORD ;;
  *) echo "uso: $0 app|migrator" >&2; exit 2 ;;
esac

[[ -f "$dir/.env" ]] || { echo "falta deploy/.env: ejecuta make dev-up" >&2; exit 1; }
pw="$(grep -E "^${key}=" "$dir/.env" | cut -d= -f2-)"
[[ -n "$pw" ]] || { echo "falta $key en deploy/.env: ejecuta make dev-env" >&2; exit 1; }

# Dentro del Dev Container el puerto publicado del host se alcanza por host.docker.internal.
host="${SEGUROS_DEV_DB_HOST:-$([[ -f /.dockerenv ]] && echo host.docker.internal || echo localhost)}"
printf 'postgres://%s:%s@%s:5432/seguros?sslmode=disable\n' "$user" "$pw" "$host"
