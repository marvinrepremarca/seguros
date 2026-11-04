#!/usr/bin/env bash
# Asegura en el Keycloak de desarrollo el cliente seguros-directory (solo desarrollo; idempotente).
# El realm solo se importa con la BD vacía: este script aplica el cambio a un Keycloak ya existente,
# sin borrar nada. Con un realm nuevo lo crea deploy/keycloak/realm-dev.json.
set -euo pipefail

dir="$(cd "$(dirname "$0")/.." && pwd)"
get() { grep -E "^$1=" "$dir/.env" | cut -d= -f2-; }
realm=seguros-dev
kc() {
  "$dir/scripts/compose.sh" exec -T keycloak /opt/keycloak/bin/kcadm.sh "$@"
}

kc config credentials --server http://localhost:8081 --realm master --user admin \
  --password "$(get KC_BOOTSTRAP_ADMIN_PASSWORD)" >/dev/null 2>&1

id="$(kc get clients -r "$realm" -q clientId=seguros-directory --fields id --format csv --noquotes 2>/dev/null | tr -d '\r')"
if [[ -z "$id" ]]; then
  kc create clients -r "$realm" -s clientId=seguros-directory -s enabled=true -s publicClient=false \
    -s standardFlowEnabled=false -s directAccessGrantsEnabled=false -s serviceAccountsEnabled=true \
    -s "secret=$(get KC_DIRECTORY_CLIENT_SECRET)" >/dev/null 2>&1
fi
kc add-roles -r "$realm" --uusername service-account-seguros-directory --cclientid realm-management \
  --rolename view-users --rolename query-users --rolename view-realm >/dev/null 2>&1
