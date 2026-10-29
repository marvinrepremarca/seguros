---
name: feature-flag
description: Registra, aplica en todas las capas, prueba y retira feature flags (módulo, funcionalidad, release, kill switch) por tenant. Úsala siempre que algo deba poder encenderse/apagarse independientemente.
---
# Feature flags: ciclo de vida completo

Sigue estrictamente `.claude/rules/feature-flags.md`.

## Crear
1. Elige tipo y clave según la nomenclatura (`module.*`, `<modulo>.*`, `release.*`, `ops.*`).
2. Agrega la entrada en `src/backend/internal/platform/flags/registry.yaml`:
   `key`, `type`, `description` (español, entendible por el admin del tenant), `default`, `requires`, `owner`, `plans`, `removeBy` (obligatorio en `release.*`).
3. `make generate` → constantes Go (`flags.X`) y TS (`features.x`).
4. La validación del registro (en `make check`) debe pasar: sin ciclos en `requires`, sin núcleo dependiendo de apagable, sin `removeBy` vencido.

## Aplicar (las 4 capas)
- API: `x-feature` en la operación OpenAPI y `flags.Require(flags.X)` en el grupo/ruta.
- Consumidores/jobs: primera instrucción `if !flags.Enabled(ctx, flags.X) { return nil }` con el tenant del mensaje en `ctx`.
- Variación de comportamiento: dos implementaciones de una interfaz y selección en un único punto (`module.go` o decorador), nunca dentro de `domain`.
- UI: `FeatureGate`, `beforeLoad` de ruta, filtro de menú.

## Probar
ON/OFF por endpoint, consumidor sin efectos con OFF, dos tenants con valores distintos, UI oculta con OFF.

## Retirar (release flags)
1. Confirma ON para todos los tenants en producción ≥ 2 semanas.
2. Elimina entrada del registro, constantes, guardias, implementación OFF y sus pruebas en un solo PR.
3. Busca residuos: la constante y la clave literal en todo `src/`.

## Prohibido
Flags sin registrar, evaluar sin tenant, flags que borren datos, usar flags como sustituto de permisos.
