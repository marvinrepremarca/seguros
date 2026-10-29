---
name: new-use-case
description: Implementa un caso de uso (comando o consulta) como slice vertical en un módulo Go existente — contrato OpenAPI, dominio, handler, autorización, adaptadores, flag, pruebas y UI. Úsala para cualquier funcionalidad de negocio nueva (ej. "registrar endoso", "aplicar pago", "reportar siniestro").
---
# Nuevo caso de uso

## 1. Entender (no escribas código aún)
- Identifica el flujo de `flujos.md` y el agregado dueño en `DOMAIN.md`.
- Lista: actor/rol y permiso, entradas, invariantes, transición de estado, eventos, efectos en dinero, datos sensibles, flag.
- Regla de negocio no documentada → pregunta al usuario o consulta `insurance-domain-expert`. **No inventes.**
- Presenta el plan y espera confirmación si toca dinero, estados o datos Restringidos.

## 2. Contrato
Agrega la operación en `api/openapi/<modulo>.yaml` (request/response, errores problem+json, `x-permission`, `x-feature`, `Idempotency-Key` si crea dinero/documentos). `make generate`.

## 3. Dominio (TDD)
- Pruebas table-driven de invariantes y transiciones (válidas e inválidas).
- Método del agregado que devuelve `error` de dominio tipado y registra el evento.

## 4. Aplicación — `app/<caso_de_uso>.go`
`<CasoDeUso>Command` + `<CasoDeUso>Handler.Handle(ctx, cmd)`:
`authz.Require` → validar → `db.WithTenantTx` { cargar agregado → invocar dominio → guardar → escribir eventos al outbox / `river.InsertTx` } → retornar resultado.
Evento de integración nuevo → `/integration-event`.

## 5. Adaptadores
- `adapters/postgres`: queries sqlc nuevas en `queries/<modulo>/`; cambio de esquema → `/db-migration`.
- `adapters/http`: implementa el método del strict server: mapea request → command, error de dominio → problem+json.

## 6. Pruebas de integración (`-tags=integration`)
2xx, 400, 403, 404 otro tenant, 404 flag OFF, idempotencia (si aplica), evento presente en outbox, respuesta válida contra el spec.

## 7. Frontend
`pnpm -C src/frontend api:generate`; hook/form/vista en el módulo según `frontend.md`.

## 8. Verificación
`make check` + pruebas de integración + frontend; reporta resultados reales. `security-auditor` si toca dinero, auth, PII o archivos.
