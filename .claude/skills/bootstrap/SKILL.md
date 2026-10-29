---
name: bootstrap
description: Construye las fundaciones del repositorio (Fase 0) y la plataforma transversal (Fase 1) del ERP de seguros siguiendo docs/ROADMAP.md, entregable por entregable y verificando cada uno. Úsala cuando el repositorio no tenga aún código, o para continuar la Fase 0/1 donde quedó.
---
# Bootstrap del proyecto (Fases 0 y 1)

## Antes de empezar
1. Lee `docs/ROADMAP.md` (tabla de seguimiento) para saber en qué entregable vas. Nunca rehagas uno marcado ✅ sin pedirlo.
2. Verifica herramientas: `go version` (≥1.25), `node -v` (≥22), `pnpm -v`, motor de contenedores con `docker compose version` o `podman compose version`, `make -v`. Si falta alguna, **detente** y di al usuario exactamente qué instalar; no busques atajos.
3. Si falta el path del módulo Go (`github.com/<org>/seguros`), pregúntalo.

## Método por entregable (repite para cada fila de la fase)
1. Anuncia el entregable (ej. "1.4 Base de datos") y los archivos que crearás.
2. Implementa siguiendo las reglas de `.claude/rules/` (arquitectura, base de datos, seguridad, pruebas, devops). Para dependencias nuevas verifica la licencia (ARCHITECTURE.md §9).
3. Escribe sus pruebas en el mismo paso.
4. Ejecuta `make check` (y `pnpm -C src/frontend check` si tocaste frontend). Reporta la salida real. No avances con algo en rojo.
5. Actualiza la tabla de seguimiento del ROADMAP y haz commit: `chore(platform): <entregable>` o `feat(<modulo>): ...`.

## Puntos de control obligatorios (pide revisión al usuario)
- Tras 0.9 (fin de Fase 0).
- Tras 1.6 (autenticación) y 1.10 (cifrado): ejecuta el subagente `security-auditor`.
- Al final de la Fase 1: ejecuta `architecture-guardian` y `security-auditor`, marca la fase ✅ solo si no hay hallazgos Altos/Críticos.

## Guías concretas
- **Money**: `Allocate(n)` reparte en n partes y asigna el residuo a la última (DOMAIN.md, PaymentPlan). Pruebas: 100.000 / 3, montos con 4 decimales, monedas distintas → error.
- **WithTenantTx**: obtiene `tenantID` del `ctx` (puesto por middleware tras autenticar); si no hay tenant → error, nunca default.
- **Roles de BD**: `seguros_migrator` (owner de esquemas) y `seguros_app` (sin `BYPASSRLS`, sin DDL). Créalos en un script de init del compose de desarrollo.
- **Keycloak dev**: realm `seguros-dev` exportado a `deploy/keycloak/realm-dev.json` con cliente confidencial del BFF, roles de DOMAIN.md §4 y usuarios de prueba; sin secretos reales.
- **Registro de flags**: incluye desde el inicio todas las claves `module.*` de ARCHITECTURE.md §1.2 con su `requires`, aunque los módulos aún no existan.

## Nunca
Saltar fases, desactivar linters/pruebas para avanzar, añadir lógica de seguros en Fase 1, guardar secretos reales en el repo.
