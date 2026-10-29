---
paths:
  - "src/**"
---
# Reglas de pruebas

## Pirámide y mínimos
| Nivel | Herramienta | Qué cubre | Mínimo |
|---|---|---|---|
| Unitarias dominio | `testing` + testify, table-driven | Invariantes, transiciones, cálculos de dinero | 100% de transiciones válidas **e inválidas**; ≥90% cobertura en `domain` |
| Unitarias app | `testing` + `go.uber.org/mock` o fakes manuales | Handlers: autorización, validación, orquestación | Feliz + cada error de negocio + sin permiso |
| Integración (`//go:build integration`) | testcontainers-go (PostgreSQL, Valkey) + `httptest` sobre el router real | Endpoint real, BD real con RLS, outbox, flags | Cada endpoint: 2xx, 400, 401/403, 404 otro tenant, 404 flag OFF |
| Arquitectura | go-arch-lint + depguard | Fronteras de módulo y capas | Siempre en CI |
| Contrato | Validación del spec (`kin-openapi`) + respuestas validadas contra OpenAPI en integración | Cambios no intencionales | Siempre |
| Frontend | Vitest + Testing Library + MSW | Componentes, hooks, FeatureGate | Lógica y formularios críticos |
| E2E | Playwright | Flujos críticos de negocio | Ver frontend.md |
| Carga | k6 | p95 < 300 ms lecturas, < 800 ms escrituras a 500 VUs | Antes de cada release mayor |

## Reglas
- Pruebas table-driven con `t.Run(nombre, ...)`; `t.Parallel()` cuando sea seguro.
- Builders de dominio en `<modulo>/domain/domaintest` (`domaintest.ValidPolicy(t, opts...)`); datos falsos con gofakeit.
- Sin mocks de la BD: integración usa PostgreSQL real con migraciones goose aplicadas una vez por paquete (`TestMain`).
- Tiempo controlado con `kernel.Clock` falso; prohibido `time.Sleep` en pruebas (usa `require.Eventually` solo para asincronía real).
- Cada prueba de integración crea su propio tenant; pruebas independientes del orden.
- Siempre `go test -race`.
- Dinero: bordes de redondeo (prima no divisible entre cuotas, prorrata por días, prima no devengada).
- Un bug corregido = una prueba que lo reproduce primero.
- No borres ni debilites pruebas para pasar el build; explica antes de cambiar una prueba.
