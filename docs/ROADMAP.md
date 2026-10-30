# Hoja de ruta de construcción

> Orden **obligatorio** de construcción. Un agente no empieza una fase hasta que la anterior cumple su *Definición de Hecho* (DoD). Al terminar cada entregable, actualiza su estado en la tabla de seguimiento (§ final) en el mismo commit.
> Para ejecutar las fases 0 y 1 usa la skill `/bootstrap`. Para las fases de negocio usa `/new-module` y `/new-use-case`.

Prerrequisitos de la estación de trabajo: **Docker Desktop**, **VS Code** con la extensión *Dev Containers* y git. Todo lo demás (Go 1.26, Node 22, pnpm, make, sqlc, oapi-codegen, goose, golangci-lint, go-arch-lint, gosec, govulncheck, go-licenses, Claude Code) viene en el **Dev Container** (`.devcontainer/`): en VS Code, *Reopen in Container*. Las versiones de las herramientas se fijan en `.devcontainer/Dockerfile`; actualizarlas es un cambio deliberado con commit propio.

---

## Fase 0 — Fundaciones del repositorio
Objetivo: que `make check` y `pnpm check` corran en verde sobre un proyecto vacío pero bien configurado.

| # | Entregable | Detalle |
|---|---|---|
| 0.1 | `src/backend/go.mod` | módulo `github.com/<org>/seguros` (preguntar `<org>` al usuario), Go 1.26 |
| 0.2 | `Makefile` raíz | targets: `generate`, `fmt`, `lint`, `arch`, `test`, `test-integration`, `check` (= fmt+lint+arch+test), `migrate-up`, `dev-up`, `dev-down` |
| 0.3 | `.golangci.yml` | linters de `.claude/rules/architecture.md` §Estilo, incluido `depguard` y `forbidigo` |
| 0.4 | `.go-arch-lint.yml` | componentes `platform`, `domain`, `app`, `adapters`, `contracts`, `cmd`; reglas de import |
| 0.5 | `sqlc.yaml`, `oapi-codegen` config | overrides `numeric→decimal.Decimal`, `uuid→uuid.UUID`, `date→kernel.Date` |
| 0.6 | `deploy/compose.dev.yml` | postgres 17, valkey, keycloak (realm importado), seaweedfs, otel-collector; `.env.example` sin valores secretos |
| 0.7 | `src/frontend` | Vite + React 19 + TS strict + TanStack Router/Query + Tailwind v4 + shadcn/ui + Vitest + Playwright + ESLint (`boundaries`) + orval; script `check` |
| 0.8 | `.github/workflows/ci.yml` | pipeline de ARCHITECTURE.md §6.4 (staging/prod como jobs deshabilitados hasta Fase 6) |
| 0.9 | `.editorconfig`, `CODEOWNERS`, plantilla de PR con checklist de reglas no negociables |

**DoD:** `make check` y `pnpm -C src/frontend check` en verde; CI en verde; `docker compose -f deploy/compose.dev.yml up -d` levanta todo sano.

---

## Fase 1 — Plataforma (sin lógica de seguros)
Objetivo: todo lo transversal que los módulos dan por hecho.

| # | Entregable | Paquete | DoD específica |
|---|---|---|---|
| 1.1 | Kernel | `internal/platform/kernel` | `Money` (decimal, moneda, `Add/Sub/Mul/Allocate` con reparto de residuo), `Date`, `DateRange`, `Clock` (+ fake), IDs UUIDv7 tipados, `DomainError` con código. Cobertura ≥95%. |
| 1.2 | Config y observabilidad | `platform/config`, `platform/obs` | Config por env validada al arrancar; `slog` JSON con redacción; OTel traces/metrics. |
| 1.3 | Servidor HTTP | `platform/httpx` | chi, timeouts, recover, request-id, problem+json, límites de body, headers de seguridad, `/health/live`, `/health/ready`. |
| 1.4 | Base de datos | `platform/db`, `cmd/migrate` | pool pgx, `WithTenantTx` (set_config), roles `app`/`migrator`, goose por módulo. Prueba: sin `WithTenantTx` no hay acceso. |
| 1.5 | Tenancy | módulo `tenancy` | Tenant, plan, estado; resolución de tenant desde la sesión. |
| 1.6 | Identidad y BFF | módulo `identity`, `platform/authz` | Login OIDC con Keycloak (PKCE), sesión scs en Valkey, cookies `__Host-`, CSRF, `authz.Require`, catálogo de permisos, prueba de ruta sin permiso = falla. |
| 1.7 | Feature flags | `platform/flags`, módulo `featuremgmt` | `registry.yaml` + generador de constantes Go/TS, proveedor OpenFeature por tenant, caché L1/L2 + invalidación, middleware `Require`, endpoint `/bff/features`, validación de `requires` y `removeBy`, auditoría de cambios. |
| 1.8 | Mensajería | `platform/outbox`, `platform/jobs` | outbox + relay Watermill, inbox idempotente, River con `InsertTx`, `cmd/worker`. |
| 1.9 | Auditoría | módulo `audit` | `audit.events` append-only particionada; API interna `audit.Record`. |
| 1.10 | Cifrado de campos | `platform/crypto` | AES-GCM envelope con versión de llave, blind index HMAC; llave maestra desde OpenBao (dev: env). |
| 1.11 | Frontend base | `src/frontend/src/app`, `shared` | layout, login vía BFF, `FeatureGate`, menú por flags/permisos, formateadores COP/fecha, validadores CC/NIT. |
| 1.12 | Prueba de humo E2E | `test/` + Playwright | login → ver menú filtrado por flags de un tenant de prueba. |

**DoD:** todo lo anterior con pruebas; `security-auditor` y `architecture-guardian` sin hallazgos Altos/Críticos.

---

## Fase 2 — Núcleo de negocio (módulos no apagables)
Orden: **Parties → Catalog → Policies**.
- `parties`: personas/organizaciones, documentos cifrados, contactos, consentimiento habeas data.
- `catalog`: aseguradoras, ramos, productos, esquemas JSON de riesgo por ramo, acuerdos de comisión.
- `policies`: póliza + máquina de estados completa, coberturas, bienes asegurados, endosos, cancelación, eventos de DOMAIN.md §3.

**DoD:** flujo 3 (expedición manual de póliza) y 5, 8 operables desde la UI; todas las transiciones probadas.

## Fase 3 — Dinero
Orden: **Billing → Commissions → Renewals**. Requiere decisiones de [docs/domain/DECISIONS.md](domain/DECISIONS.md) cerradas para estos módulos.

## Fase 4 — Venta y servicio
Orden: **Crm → Quoting → Claims**.

## Fase 5 — Transversales
**Compliance → Documents → Notifications → Reporting → Integrations.Insurers** (una aseguradora piloto primero).

## Fase 6 — Endurecimiento y salida a producción
Pruebas k6 (objetivos en testing.md), ZAP, restauración de backup probada, runbooks, despliegue en VPS, monitoreo y alertas, onboarding del primer tenant real.

---

## Seguimiento

| Ítem | Estado | Fecha | Notas |
|---|---|---|---|
| Fase 0 | ✅ Hecho | 2026-10-30 | DoD verificada: make check, pnpm check, compose sano y CI en verde (PR #1) |
| Fase 1 | ⬜ Pendiente | | |
| Fase 2 | ⬜ Pendiente | | |
| Fase 3 | ⬜ Pendiente | | |
| Fase 4 | ⬜ Pendiente | | |
| Fase 5 | ⬜ Pendiente | | |
| Fase 6 | ⬜ Pendiente | | |

Estados: ⬜ Pendiente · 🟨 En curso · ✅ Hecho (DoD verificada) · ⛔ Bloqueado (indicar motivo).
