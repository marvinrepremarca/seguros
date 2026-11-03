-- Esquema de River (jobs, edición OSS). Las tablas las crea y versiona el propio River
-- (rivermigrate, en river.river_migration) desde cmd/migrate, justo después de las migraciones
-- goose; aquí solo se crea el esquema y los permisos por defecto para el rol de la app.
-- Las tablas de River no son datos de un tenant (sin RLS): los argumentos de los jobs solo llevan
-- IDs y el tenant_id, y cada worker abre db.WithTenantTx con ese tenant.
-- +goose Up
CREATE SCHEMA river;
GRANT USAGE ON SCHEMA river TO seguros_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA river GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO seguros_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA river GRANT USAGE, SELECT ON SEQUENCES TO seguros_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA river GRANT EXECUTE ON FUNCTIONS TO seguros_app;

-- +goose Down
-- Revertir exige antes `river migrate down` (las tablas pertenecen a River).
ALTER DEFAULT PRIVILEGES IN SCHEMA river REVOKE ALL ON TABLES FROM seguros_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA river REVOKE ALL ON SEQUENCES FROM seguros_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA river REVOKE ALL ON FUNCTIONS FROM seguros_app;
DROP SCHEMA river;
