-- Llaves de datos (DEK) por tenant para el cifrado de campos (ARCHITECTURE.md §4.4). Cada DEK se
-- guarda ENVUELTA por la llave maestra (KEK), que vive fuera de la BD (OpenBao; en desarrollo, una
-- variable de entorno). Sin la KEK estas filas no permiten descifrar nada.
-- +goose Up
CREATE SCHEMA crypto;
GRANT USAGE ON SCHEMA crypto TO seguros_app;

CREATE TABLE crypto.data_keys (
    tenant_id  uuid        NOT NULL,
    version    integer     NOT NULL CHECK (version > 0),
    wrapped    bytea       NOT NULL CHECK (octet_length(wrapped) BETWEEN 16 AND 1024),
    kek_ref    text        NOT NULL CHECK (char_length(kek_ref) BETWEEN 1 AND 64),
    status     text        NOT NULL CHECK (status IN ('active', 'retired')),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, version)
);
-- Una sola DEK activa por tenant (las retiradas siguen descifrando datos antiguos).
CREATE UNIQUE INDEX data_keys_one_active ON crypto.data_keys (tenant_id) WHERE status = 'active';

ALTER TABLE crypto.data_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE crypto.data_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crypto.data_keys
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- Sin DELETE: borrar una DEK haría ilegibles los datos que cifró.
GRANT SELECT, INSERT, UPDATE ON crypto.data_keys TO seguros_app;

-- +goose Down
DROP TABLE crypto.data_keys;
DROP SCHEMA crypto;
