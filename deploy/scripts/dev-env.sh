#!/usr/bin/env bash
# Crea o completa deploy/.env a partir de deploy/.env.example con valores aleatorios
# para las variables vacías. No sobrescribe valores existentes. Solo para desarrollo.
set -euo pipefail

dir="$(cd "$(dirname "$0")/.." && pwd)"
example="$dir/.env.example"
env="$dir/.env"

touch "$env"
chmod 600 "$env"

while IFS= read -r line; do
  [[ "$line" =~ ^([A-Z0-9_]+)=$ ]] || continue
  key="${BASH_REMATCH[1]}"
  if ! grep -qE "^${key}=.+" "$env"; then
    sed -i "/^${key}=/d" "$env"
    printf '%s=%s\n' "$key" "$(openssl rand -hex 24)" >> "$env"
    echo "generado: $key"
  fi
done < "$example"
