---
paths:
  - "src/backend/**"
---
# Reglas de arquitectura backend (Go)

## Dependencias (verificadas por go-arch-lint + depguard en CI)
- `domain` importa solo stdlib y `internal/platform/kernel`. Sin pgx, sin net/http, sin Watermill, sin tags de JSON/DB.
- `app` importa `domain` y define sus **puertos** (interfaces) en el propio paquete. No conoce pgx ni HTTP.
- `adapters/*` implementan los puertos de `app`.
- `adapters/http` solo mapea HTTP ↔ comandos/consultas. **Cero lógica de negocio en handlers HTTP.**
- Entre módulos: únicamente `internal/modules/<otro>/contracts`. Si necesitas datos de otro módulo: (1) evento de integración y proyección local, o (2) interfaz de consulta publicada en su `contracts` e inyectada en `module.go`. Nunca acceso a sus tablas.
- Prohibido paquetes `common`, `utils`, `helpers` dentro de módulos. Lo compartido de verdad va a `internal/platform` con justificación.
- Sin variables globales mutables ni `init()` con efectos. Wiring explícito en `module.go` y `cmd/*` (sin frameworks de DI por reflexión).

## Dominio
- Agregados como structs con campos **no exportados**; construcción solo vía `New<Agregado>(...) (*Agregado, error)` que valida invariantes; rehidratación desde BD vía `Rehydrate<Agregado>(...)` sin validaciones de creación.
- Value objects inmutables (pasados por valor): `Money`, `PolicyNumber`, `DocumentID` (tipo + número), `DateRange`, `Percentage`, `Email`, `Phone`.
- IDs tipados (`type PolicyID uuid.UUID`) con UUIDv7 (`google/uuid`).
- Errores de negocio: variables/tipos con código estable (`policies.endorsement.outside_coverage_period`) implementando `kernel.DomainError`; comparar con `errors.Is/As`.
- Domain events se acumulan en el agregado (`PullEvents()`) y el handler los escribe al outbox en la misma transacción.
- Máquinas de estado: tabla de transiciones explícita (`map[Status][]Status`) y método `transitionTo` privado.

## Aplicación
- Un caso de uso por archivo dentro de `app/`: `issue_policy.go` con `IssuePolicyCommand`, `IssuePolicyHandler` y su `Handle(ctx, cmd) (Result, error)`.
- Commands modifican; queries no modifican y usan queries sqlc de solo lectura.
- Toda operación autoriza explícitamente (permiso + propiedad) antes de tocar el dominio, usando `authz.Require(ctx, perm)`.
- Unidad de trabajo: `db.WithTenantTx(ctx, func(tx pgx.Tx) error {...})` envuelve carga → dominio → guardado → outbox.
- Consumidores de eventos: idempotentes, verifican flag del tenant, nunca devuelven error por "no aplica".

## API (OpenAPI-first)
- El contrato `api/openapi/<modulo>.yaml` se escribe **antes** que el handler; `oapi-codegen` genera la interfaz strict server.
- Rutas: `/api/v1/<modulo>/<recurso>` kebab-case plural.
- Errores: `application/problem+json` (RFC 9457) con `type` = código estable, `traceId`, sin detalles internos.
- Paginación keyset (cursor opaco), límite máximo 200.
- POST que crean dinero o documentos aceptan `Idempotency-Key`.
- Cada grupo de rutas aplica middlewares en orden: recover → request-id/otel → auth (sesión BFF) → tenant → `flags.Require` → rate limit; cada operación declara su permiso.

## Rendimiento y concurrencia
- `context.Context` primer parámetro en todo lo que haga I/O; respeta cancelación y timeouts.
- Prohibido N+1: queries sqlc con JOINs/`ANY($1)` dentro del esquema del módulo.
- Nada pesado en el request (PDF, emails, llamadas a aseguradoras): encola job River o evento.
- Goroutines siempre con dueño y fin (`errgroup`); nunca `go func()` sueltas en handlers.
- Clientes HTTP externos: `http.Client` con timeout, retry con jitter y circuit breaker.
- Ejecuta pruebas con `-race`.

## Estilo
- `gofmt`/`goimports`, golangci-lint con: errcheck, govet, staticcheck, gosec, revive, depguard, exhaustive (switch de estados), forbidigo (`time.Now`, `float64` en paquetes de dinero, `fmt.Print*`).
