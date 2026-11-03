-- Registro de tenants (agencias). La fila de un tenant es visible solo dentro de su propio
-- contexto (RLS sobre id). La traducción Organization de Keycloak → tenant, que ocurre antes de
-- conocer el tenant (login), se hace únicamente con la función SECURITY DEFINER de abajo.
-- +goose Up
CREATE SCHEMA tenancy;
GRANT USAGE ON SCHEMA tenancy TO seguros_app;

CREATE TABLE tenancy.tenants (
    id            uuid PRIMARY KEY,
    name          text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    plan          text NOT NULL CHECK (plan IN ('basico', 'profesional', 'empresarial')),
    status        text NOT NULL CHECK (status IN ('active', 'suspended')),
    idp_org_alias text NOT NULL CHECK (idp_org_alias ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'),
    status_since  timestamptz NOT NULL,
    created_at    timestamptz NOT NULL,
    created_by    uuid,
    updated_at    timestamptz NOT NULL,
    updated_by    uuid,
    version       integer NOT NULL CHECK (version > 0),
    CONSTRAINT tenants_idp_org_alias_key UNIQUE (idp_org_alias)
);

ALTER TABLE tenancy.tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenancy.tenants FORCE ROW LEVEL SECURITY;
-- Esta tabla ES el registro de tenants: la columna de aislamiento es su PK.
CREATE POLICY tenant_isolation ON tenancy.tenants
    USING (id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (id = current_setting('app.tenant_id')::uuid);
-- Lectura completa solo para el dueño (rol de migraciones), que es quien ejecuta la función
-- SECURITY DEFINER. El rol de la app no recibe esta política.
CREATE POLICY owner_lookup ON tenancy.tenants FOR SELECT TO CURRENT_USER USING (true);

GRANT SELECT, INSERT, UPDATE ON tenancy.tenants TO seguros_app;

-- Devuelve solo id y estado del tenant vinculado a una Organization (sin nombre ni plan).
CREATE FUNCTION tenancy.resolve_tenant_by_org_alias(p_alias text)
RETURNS TABLE (id uuid, status text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT t.id, t.status FROM tenancy.tenants t WHERE t.idp_org_alias = p_alias
$$;
REVOKE ALL ON FUNCTION tenancy.resolve_tenant_by_org_alias(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION tenancy.resolve_tenant_by_org_alias(text) TO seguros_app;

-- +goose Down
DROP FUNCTION tenancy.resolve_tenant_by_org_alias(text);
DROP TABLE tenancy.tenants;
DROP SCHEMA tenancy;
