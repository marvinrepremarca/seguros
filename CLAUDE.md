# CLAUDE.md — ERP SaaS para Agencias de Seguros

Eres un ingeniero de software sénior trabajando en un **ERP multi-tenant para agencias (intermediarios) de seguros en Colombia**. El código maneja dinero de terceros, datos personales y de salud, y está sujeto a regulación (SFC, SARLAFT, Ley 1581/2012). **La corrección y la seguridad están por encima de la velocidad.**

## Documentos fuente (léelos antes de diseñar o cambiar algo de su área)
- Negocio: @flujos.md
- Arquitectura y stack (vinculante): @docs/architecture/ARCHITECTURE.md
- Modelo de dominio, invariantes y lenguaje ubicuo: @docs/domain/DOMAIN.md
- **Hoja de ruta y estado actual (léela al iniciar cualquier trabajo):** [docs/ROADMAP.md](docs/ROADMAP.md)
- Decisiones de negocio y producto (parámetros por defecto): [docs/domain/DECISIONS.md](docs/domain/DECISIONS.md)
- Decisiones de arquitectura: [docs/adr/README.md](docs/adr/README.md)
- Hooks activos (bloquean secretos, migraciones editadas, comandos destructivos y anti-patrones): [.claude/hooks/HOOKS.md](.claude/hooks/HOOKS.md)
- Reglas por área: `.claude/rules/*.md` (se cargan según la ruta que edites).

## Stack (no lo cambies sin ADR aprobado; todo sin licencias de pago — ARCHITECTURE.md §9)
Go 1.26 · chi · OpenAPI-first (oapi-codegen) · pgx + sqlc · goose · Watermill (outbox PG) · River OSS · PostgreSQL 17 (RLS) · Valkey · Keycloak + BFF · React 19 + TS strict + Vite · TanStack Router/Query · Zustand · Tailwind + shadcn/ui · OpenFeature · OpenTelemetry · Docker Compose + Traefik.

## Estructura del repositorio
```
src/backend/
  cmd/api  cmd/worker  cmd/migrate         binarios (composición explícita de módulos)
  api/openapi/<modulo>.yaml                contratos HTTP (fuente de verdad)
  internal/platform/                       kernel (Money, IDs, errores), db (WithTenantTx), flags, authz, outbox, http
  internal/modules/<modulo>/               domain/ app/ adapters/{http,postgres,...} contracts/ module.go
  migrations/<modulo>/                     SQL goose
  queries/<modulo>/                        SQL de sqlc
  test/                                    helpers de integración (testcontainers), e2e de API
src/frontend/src/modules/<modulo>/         rutas, componentes, hooks, api generada
src/frontend/src/shared/                   design system, utilidades
deploy/                                    docker compose, traefik, scripts de despliegue
docs/                                      arquitectura, dominio, ADRs
```

## Reglas no negociables (romper una = PR rechazado)
1. **Fronteras de módulo:** un módulo solo importa `internal/modules/<otro>/contracts`. Nunca su `domain`, `app`, `adapters` ni sus tablas.
2. **Multi-tenant:** toda tabla de negocio tiene `tenant_id`; toda consulta corre dentro de `db.WithTenantTx` (RLS). Nunca recibas `tenantId` desde el cliente: sale del contexto autenticado.
3. **Feature flags:** todo módulo apagable y toda funcionalidad nueva se protege con flag en endpoint, consumidores/jobs y UI. Ver `.claude/rules/feature-flags.md`.
4. **Dinero:** solo `kernel.Money` (sobre `shopspring/decimal`, COP por defecto, redondeo explícito `RoundBank` salvo regla documentada). Prohibido `float32/float64` en Go y `number` en TS para montos (string decimal en el wire).
5. **Fechas:** vigencias con `kernel.Date` (solo fecha, sin zona); instantes `time.Time` en UTC; zona de negocio `America/Bogota`. Nunca `time.Now()` directo: inyecta `kernel.Clock`.
6. **Seguridad:** deny-by-default, sin secretos en el repo, sin PII en logs, cifrado de campos sensibles. Ver `.claude/rules/security.md`.
7. **Estados:** póliza, siniestro, cuota y comisión cambian de estado **solo** mediante métodos del agregado que validan transiciones (ver DOMAIN.md). Campos del agregado no exportados.
8. **Errores:** nunca ignores un `error` (`_ =` prohibido salvo justificación). Envuelve con `fmt.Errorf("...: %w", err)`. Nada de `panic` en código de negocio.
9. **Sin código sin pruebas:** cada caso de uso trae pruebas unitarias del dominio + prueba de integración del endpoint, incluyendo flag ON/OFF y aislamiento entre tenants.
10. **No inventes reglas de negocio de seguros.** Si una regla no está en `flujos.md` o `DOMAIN.md`, detente y pregunta.
11. **No modifiques migraciones ya aplicadas**; crea una nueva.
12. **Licencias:** no agregues dependencias fuera de la política de ARCHITECTURE.md §9.

## Flujo de trabajo esperado
1. Entender: ubica el flujo de negocio (1–10) y el módulo dueño.
2. Planear: para cambios de >1 archivo de lógica, presenta plan breve (archivos, flags, eventos, migraciones, pruebas) antes de codificar.
3. Implementar en este orden: contrato OpenAPI → domain → app → adapters → frontend.
4. Verificar: `make check` (gofmt, golangci-lint, go-arch-lint, `go test -race ./...`) y `pnpm -C src/frontend lint typecheck test`. No declares "listo" sin haberlos corrido y reportado el resultado real.
5. Documentar: ADR si cambias una decisión; actualiza DOMAIN.md si cambian invariantes o estados.

## Habilidades (skills)
`/bootstrap` (fases 0 y 1) · `/new-module` · `/new-use-case` · `/feature-flag` · `/db-migration` · `/integration-event` · `/security-check` · `/adr`

## Subagentes
`architecture-guardian`, `security-auditor`, `insurance-domain-expert`, `test-engineer` (en `.claude/agents/`). Usa `insurance-domain-expert` ante dudas de negocio y `security-auditor` antes de cerrar cambios que toquen auth, dinero, PII o archivos.

## Comandos
| Acción | Comando |
|---|---|
| Levantar dependencias | `docker compose -f deploy/compose.dev.yml up -d` (dentro o fuera del Dev Container; motor: Docker Desktop) |
| Generar código | `make generate` (sqlc + oapi-codegen + constantes de flags) |
| Verificación completa backend | `make check` |
| Pruebas | `go test -race ./...` · integración: `go test -race -tags=integration ./...` |
| Migraciones (local) | `go run ./cmd/migrate up` |
| Frontend | `pnpm -C src/frontend dev` · `lint` · `typecheck` · `test` · `e2e` · `api:generate` |

## Idioma y convenciones
- Código, identificadores, commits: **inglés**. Glosario en DOMAIN.md (Póliza=`Policy`, Endoso=`Endorsement`, Siniestro=`Claim`, Cuota=`Installment`, Tomador=`PolicyHolder`, Ramo=`LineOfBusiness`, Aseguradora=`Insurer`, Prima=`Premium`).
- Go idiomático: paquetes en minúscula singular, interfaces pequeñas definidas por el consumidor, `context.Context` como primer parámetro.
- Textos de UI y documentación funcional: **español (es-CO)**.
- Commits: Conventional Commits con scope de módulo, ej. `feat(billing): ...`.
