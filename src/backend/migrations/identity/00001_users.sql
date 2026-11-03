-- Usuarios de cada tenant, sincronizados desde Keycloak al iniciar sesión (aprovisionamiento JIT).
-- Keycloak es la fuente de verdad de credenciales y roles; aquí solo vive el vínculo
-- (tenant, sujeto del IdP) → id interno (UUIDv7) que usan created_by/updated_by y la auditoría.
-- email y display_name son datos Confidenciales: nunca se registran en logs.
-- +goose Up
CREATE SCHEMA identity;
GRANT USAGE ON SCHEMA identity TO seguros_app;

CREATE TABLE identity.users (
    id            uuid PRIMARY KEY,
    tenant_id     uuid NOT NULL,
    idp_subject   text NOT NULL CHECK (char_length(idp_subject) BETWEEN 1 AND 255),
    email         text CHECK (char_length(email) <= 254),
    display_name  text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 200),
    last_login_at timestamptz NOT NULL,
    created_at    timestamptz NOT NULL,
    created_by    uuid,
    updated_at    timestamptz NOT NULL,
    updated_by    uuid,
    version       integer NOT NULL CHECK (version > 0),
    CONSTRAINT users_tenant_subject_key UNIQUE (tenant_id, idp_subject)
);

ALTER TABLE identity.users ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.users FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON identity.users
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

GRANT SELECT, INSERT, UPDATE ON identity.users TO seguros_app;

-- +goose Down
DROP TABLE identity.users;
DROP SCHEMA identity;
