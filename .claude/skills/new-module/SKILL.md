---
name: new-module
description: Crea un nuevo módulo (bounded context) Go del ERP de seguros con sus paquetes hexagonales, esquema de BD, contrato OpenAPI, flag de módulo, wiring en cmd/, reglas de arquitectura y módulo frontend. Úsala cuando el usuario pida un módulo/área funcional nueva.
---
# Crear un módulo nuevo

## 0. Precondiciones (detente si falla alguna)
- El módulo corresponde a un flujo de `flujos.md` o a un ADR aprobado.
- Existe (o creas primero) su sección en `docs/domain/DOMAIN.md`: glosario, agregados, invariantes, estados, eventos publicados/consumidos, permisos.
- Decidido si es **núcleo** (sin flag de apagado) o **apagable** (`module.<nombre>`). Un núcleo no depende de un apagable.

## 1. Backend — `src/backend/internal/modules/<modulo>/`
```
domain/            agregados, value objects, errores, eventos
domain/domaintest/ builders para pruebas
app/               casos de uso + puertos (interfaces)
adapters/http/     implementación del strict server generado
adapters/postgres/ repositorios sobre código sqlc
contracts/         events.go (integration events), queries.go (interfaces públicas)
module.go          func New(deps Deps) *Module; Routes(r chi.Router); Subscribers(); Jobs()
```
- Contrato: `api/openapi/<modulo>.yaml` con `x-permission` y `x-feature` por operación; añádelo a la config de oapi-codegen.
- sqlc: `queries/<modulo>/*.sql` y entrada en `sqlc.yaml` con overrides (`numeric` → `decimal.Decimal`, `uuid` → `uuid.UUID`).
- Wiring: registra el módulo en la lista explícita de `cmd/api` y `cmd/worker`. El grupo `/api/v1/<modulo>` monta `flags.Require(flags.<Modulo>Module)`.

## 2. Feature flag → `/feature-flag` (registrar `module.<modulo>` con `requires`).
## 3. Base de datos → `/db-migration` (esquema, tablas, RLS, índices).
## 4. Permisos
Permisos `<modulo>.<recurso>.<accion>` en el catálogo de `internal/platform/authz` y asignación por rol base (DOMAIN.md §4).

## 5. Arquitectura y pruebas
- Añade el módulo a `.go-arch-lint.yml` (componentes `domain`, `app`, `adapters`, `contracts` y reglas de import) y a `depguard`.
- Prueba de integración: flag OFF → 404 en el grupo de rutas; tenant B no ve datos de A.

## 6. Frontend — `src/frontend/src/modules/<modulo>/`
`index.ts` exporta rutas lazy + entrada de menú con `flag` y `permission`; `FeatureGate` en la raíz.

## 7. Documentación
ARCHITECTURE.md §1.2 (fila del módulo). Tecnología/patrón nuevo → `/adr`.

## Checklist de salida
- [ ] `make generate` sin diffs pendientes; `make check` en verde (reporta salida real)
- [ ] go-arch-lint incluye el módulo
- [ ] Flag aplicado en API, consumidores/jobs y UI
- [ ] RLS en todas las tablas
- [ ] DOMAIN.md y ARCHITECTURE.md actualizados
