#!/usr/bin/env bash
# Exige MFA en Keycloak según el rol (DECISIONS N13). Crea un flujo de navegador que pide OTP a quien
# tenga el rol MfaRequired; quien aún no tiene segundo factor debe configurarlo al entrar (CONFIGURE_TOTP).
# Idempotente. Requiere kcadm ya autenticado y python3.
#
# Uso: deploy/scripts/keycloak-mfa.sh <realm> <rol> [<rol> ...]
#   Producción: keycloak-mfa.sh <realm> TenantAdmin Manager Operations Accounting ComplianceOfficer
# KCADM es el comando de kcadm.sh; por defecto, el del Compose de desarrollo.
set -euo pipefail

dir="$(cd "$(dirname "$0")" && pwd)"
realm="${1:?falta el realm}"
shift
[[ $# -gt 0 ]] || { echo "falta al menos un rol" >&2; exit 1; }
read -r -a kcadm <<<"${KCADM:-$dir/compose.sh exec -T keycloak /opt/keycloak/bin/kcadm.sh}"
kc() { "${kcadm[@]}" "$@"; }

flow=browser-mfa-por-rol
forms=browser-mfa-formularios
second=browser-mfa-segundo-factor
marker=MfaRequired

kc get "roles/$marker" -r "$realm" >/dev/null 2>&1 ||
  kc create roles -r "$realm" -s "name=$marker" -s "description=Exige segundo factor al ingresar" >/dev/null

# Cada rol contiene al marcador: quien tenga el rol lo tiene de forma efectiva.
for role in "$@"; do
  kc add-roles -r "$realm" --rname "$role" --rolename "$marker" >/dev/null
done

kc update "authentication/required-actions/CONFIGURE_TOTP" -r "$realm" -s enabled=true >/dev/null

if ! kc get authentication/flows -r "$realm" --fields alias --format csv --noquotes | tr -d '\r' | grep -qx "$flow"; then
  kc create authentication/flows -r "$realm" -s "alias=$flow" -s providerId=basic-flow -s topLevel=true -s builtIn=false >/dev/null
  kc create "authentication/flows/$flow/executions/execution" -r "$realm" -s provider=auth-cookie >/dev/null
  kc create "authentication/flows/$flow/executions/flow" -r "$realm" -s "alias=$forms" -s type=basic-flow -s provider=registration-page-form >/dev/null
  kc create "authentication/flows/$forms/executions/execution" -r "$realm" -s provider=auth-username-password-form >/dev/null
  kc create "authentication/flows/$forms/executions/flow" -r "$realm" -s "alias=$second" -s type=basic-flow -s provider=registration-page-form >/dev/null
  kc create "authentication/flows/$second/executions/execution" -r "$realm" -s provider=conditional-user-role >/dev/null
  kc create "authentication/flows/$second/executions/execution" -r "$realm" -s provider=auth-otp-form >/dev/null

  require() { # <flujo que la contiene> <proveedor o alias> <requisito>
    kc get "authentication/flows/$1/executions" -r "$realm" |
      python3 -c '
import json, sys
for e in json.load(sys.stdin):
    if sys.argv[1] in (e.get("providerId"), e.get("displayName")):
        e["requirement"] = sys.argv[2]
        print(json.dumps(e))
        break
else:
    sys.exit("ejecución no encontrada: " + sys.argv[1])' "$2" "$3" |
      kc update "authentication/flows/$1/executions" -r "$realm" -f - >/dev/null
  }
  require "$flow" auth-cookie ALTERNATIVE
  require "$flow" "$forms" ALTERNATIVE
  require "$forms" auth-username-password-form REQUIRED
  require "$forms" "$second" CONDITIONAL
  require "$second" conditional-user-role REQUIRED
  require "$second" auth-otp-form REQUIRED

  condition="$(kc get "authentication/flows/$second/executions" -r "$realm" |
    python3 -c 'import json,sys; print(next(e["id"] for e in json.load(sys.stdin) if e.get("providerId") == "conditional-user-role"))')"
  kc create "authentication/executions/$condition/config" -r "$realm" -s "alias=$marker" \
    -s "config.condUserRole=$marker" -s config.negate=false >/dev/null

  # Sin referencia, el mapper de amr no informa el factor y la API no puede comprobar que se usó.
  reference() { # <flujo> <proveedor> <valor>
    id="$(kc get "authentication/flows/$1/executions" -r "$realm" |
      python3 -c 'import json,sys; print(next(e["id"] for e in json.load(sys.stdin) if e.get("providerId") == sys.argv[1]))' "$2")"
    printf '{"alias":"amr-%s","config":{"default.reference.value":"%s"}}' "$3" "$3" |
      kc create "authentication/executions/$id/config" -r "$realm" -f - >/dev/null
  }
  reference "$forms" auth-username-password-form pwd
  reference "$second" auth-otp-form otp
fi

kc update "realms/$realm" -s "browserFlow=$flow" >/dev/null
