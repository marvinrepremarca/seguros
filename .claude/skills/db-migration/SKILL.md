---
name: db-migration
description: Crea migraciones goose (SQL) seguras por módulo con RLS, índices, expand/contract, queries sqlc y revisión de impacto. Úsala para cualquier cambio de esquema PostgreSQL.
---
# Migración de base de datos segura

1. **Clasifica el cambio**: aditivo (tabla/columna nullable/índice) o destructivo (drop, rename, cambio de tipo, NOT NULL sobre datos existentes). Destructivo ⇒ expand/contract en PRs y despliegues distintos; explica el plan al usuario.
2. **Crea el archivo**: `goose -dir src/backend/migrations/<modulo> create <descripcion> sql`.
3. **Escribe Up y Down** respetando `.claude/rules/database.md`: `numeric(19,4)` para dinero, `date` para vigencias, `tenant_id`, auditoría, `version`, `CHECK` de estados, y para tablas nuevas `ENABLE/FORCE ROW LEVEL SECURITY` + `CREATE POLICY tenant_isolation`.
4. **Índices grandes**: archivo separado con `-- +goose NO TRANSACTION` y `CREATE INDEX CONCURRENTLY`.
5. **Queries**: actualiza/añade `queries/<modulo>/*.sql` filtrando por `tenant_id`; `make generate` (sqlc) y corrige compilación.
6. **Revisa impacto**: locks sobre tablas grandes, defaults volátiles, pérdida de datos, reversibilidad.
7. **Pruebas**: la suite de integración aplica todas las migraciones desde cero; añade prueba de aislamiento RLS para tablas nuevas.
8. **Nunca** edites migraciones mergeadas ni ejecutes contra producción desde el agente.
