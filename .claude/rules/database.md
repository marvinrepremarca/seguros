---
paths:
  - "src/backend/migrations/**"
  - "src/backend/queries/**"
  - "src/backend/internal/**/adapters/postgres/**"
  - "**/*.sql"
---
# Reglas de base de datos (PostgreSQL + pgx + sqlc + goose)

## Esquema
- Un esquema por módulo (`policies`, `billing`, ...). Migraciones en `migrations/<modulo>/`, queries en `queries/<modulo>/`.
- Nombres `snake_case`. PK `uuid` (UUIDv7 generado en app).
- Toda tabla de negocio: `tenant_id uuid not null`, `created_at timestamptz`, `created_by uuid`, `updated_at`, `updated_by`, `version int` (concurrencia optimista: `UPDATE ... WHERE id=$1 AND version=$2`).
- Dinero: `numeric(19,4)` + `currency char(3)`; en sqlc mapear `numeric` a `shopspring/decimal` (override en `sqlc.yaml`). Porcentajes `numeric(9,6)`. Nunca `real`/`double precision`/`money`.
- Vigencias `date`; instantes `timestamptz`.
- Estados `text` + `CHECK`. No enums nativos de PG.
- Datos de riesgo por ramo: `jsonb` validado contra esquema versionado (`risk_schema_version`); índices GIN solo sobre rutas consultadas.
- Columnas cifradas: `bytea` + `<campo>_bidx bytea` (blind index) si se busca por él.

## Multi-tenancy y RLS (obligatorio)
Toda tabla nueva con `tenant_id` incluye en la misma migración:
```sql
ALTER TABLE <schema>.<tabla> ENABLE ROW LEVEL SECURITY;
ALTER TABLE <schema>.<tabla> FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON <schema>.<tabla>
  USING (tenant_id = current_setting('app.tenant_id')::uuid)
  WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
```
- Acceso a BD **solo** vía `db.WithTenantTx` (fija `set_config('app.tenant_id', $1, true)`). Prohibido usar el pool directamente en módulos.
- Las queries sqlc además filtran por `tenant_id = $1` (defensa en profundidad).
- Índices compuestos empiezan por `tenant_id`; unicidades de negocio incluyen `tenant_id`.
- El rol de la app no es owner ni tiene `BYPASSRLS`; migraciones corren con rol distinto (`cmd/migrate`).
- Prueba de integración obligatoria por tabla: datos de tenant A invisibles para tenant B.

## Migraciones (goose, SQL puro)
- Archivo `NNNNN_descripcion.sql` con secciones `-- +goose Up` y `-- +goose Down`.
- Nunca editar una migración aplicada. Cambios destructivos: **expand → migrar datos → contract** en despliegues separados.
- Índices en tablas grandes: `CREATE INDEX CONCURRENTLY` con `-- +goose NO TRANSACTION`.
- Toda migración reversible o documentar por qué no.
- La app nunca migra al arrancar en producción.

## Queries
- Solo SQL en archivos `.sql` de sqlc (parametrizado). Prohibido construir SQL con `fmt.Sprintf`/concatenación. Filtros dinámicos: `sqlc.narg` o `COALESCE`.
- Transacción = un agregado + outbox. Varios agregados → eventos.
- `SELECT ... FOR UPDATE` solo en aplicación de pagos, con justificación.
- `audit.events`: solo `INSERT` para el rol de app; particionada por mes.
