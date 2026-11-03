-- Revisión de arquitectura de la Fase 1 (Medio 2): alta, plan y suspensión de tenants son
-- operaciones de super-administrador (cmd/platformctl, rol seguros_platform). La API solo lee su
-- propio tenant (RLS) y resuelve la organización con la función SECURITY DEFINER.
-- +goose Up
REVOKE INSERT, UPDATE ON tenancy.tenants FROM seguros_app;
GRANT INSERT, UPDATE ON tenancy.tenants TO seguros_platform;

-- +goose Down
REVOKE INSERT, UPDATE ON tenancy.tenants FROM seguros_platform;
GRANT INSERT, UPDATE ON tenancy.tenants TO seguros_app;
