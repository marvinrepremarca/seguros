#!/usr/bin/env bash
# Envoltorio de `docker compose` para el entorno de desarrollo.
# Dentro del Dev Container (docker-outside-of-docker) el daemon resuelve los bind mounts en el
# HOST, así que se detecta la ruta real del repositorio en el host y se exporta como
# LOCAL_WORKSPACE_FOLDER (la usa compose.dev.yml). Fuera del contenedor no hace falta.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"

if [[ -z "${LOCAL_WORKSPACE_FOLDER:-}" && -f /.dockerenv ]]; then
  host_path="$(docker inspect "$(hostname)" \
    --format "{{range .Mounts}}{{if eq .Destination \"$root\"}}{{.Source}}{{end}}{{end}}" 2>/dev/null || true)"
  if [[ -z "$host_path" ]]; then
    echo "No se pudo detectar la ruta del repositorio en el host; define LOCAL_WORKSPACE_FOLDER." >&2
    exit 1
  fi
  export LOCAL_WORKSPACE_FOLDER="$host_path"
fi

exec docker compose -f "$root/deploy/compose.dev.yml" "$@"
