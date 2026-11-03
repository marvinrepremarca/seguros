#!/usr/bin/env bash
# Envoltorio de `docker compose` para el entorno de desarrollo.
# Dentro del Dev Container (docker-outside-of-docker) el daemon resuelve los bind mounts en el
# HOST, así que se detecta dónde vive el repositorio para el daemon y se exporta como
# LOCAL_WORKSPACE_FOLDER (la usa compose.dev.yml). Fuera del contenedor no hace falta.
#
# Soporta los dos modos del Dev Container:
#   - carpeta del host montada (bind): el montaje cuyo destino es la raíz del repo;
#   - "Clone Repository in Container Volume": un volumen montado en un directorio padre
#     (ej. /workspaces); la ruta es la del volumen en el daemon + el resto de la ruta.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"

if [[ -z "${LOCAL_WORKSPACE_FOLDER:-}" && -f /.dockerenv ]]; then
  # Una línea por montaje: destino<TAB>origen. Se elige el destino más largo que contenga al repo.
  mounts="$(docker inspect "$(hostname)" \
    --format '{{range .Mounts}}{{.Destination}}{{"\t"}}{{.Source}}{{"\n"}}{{end}}' 2>/dev/null || true)"
  best_dest="" best_src=""
  while IFS=$'\t' read -r dest src; do
    [[ -n "$dest" && -n "$src" ]] || continue
    if [[ "$root" == "$dest" || "$root" == "$dest"/* ]] && ((${#dest} > ${#best_dest})); then
      best_dest="$dest" best_src="$src"
    fi
  done <<< "$mounts"
  if [[ -z "$best_dest" ]]; then
    echo "No se pudo detectar la ruta del repositorio en el host; define LOCAL_WORKSPACE_FOLDER." >&2
    exit 1
  fi
  export LOCAL_WORKSPACE_FOLDER="${best_src}${root#"$best_dest"}"
fi

exec docker compose -f "$root/deploy/compose.dev.yml" "$@"
