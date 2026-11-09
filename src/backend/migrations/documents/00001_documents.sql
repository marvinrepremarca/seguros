-- Gestión documental: documentos con versiones, antivirus previo a la descarga y revisión de retención.
-- +goose Up
CREATE SCHEMA documents;
GRANT USAGE ON SCHEMA documents TO seguros_app;

CREATE TABLE documents.documents (
    id              uuid PRIMARY KEY,
    tenant_id       uuid NOT NULL,
    owner_type      text NOT NULL CHECK (owner_type IN ('party', 'policy', 'claim')),
    owner_id        uuid NOT NULL,
    title           text NOT NULL CHECK (char_length(title) BETWEEN 2 AND 200),
    sensitive       boolean NOT NULL,
    idempotency_key text CHECK (char_length(idempotency_key) <= 100),
    current_number  integer NOT NULL CHECK (current_number > 0),
    created_on      date NOT NULL,
    created_at      timestamptz NOT NULL,
    created_by      uuid,
    updated_at      timestamptz NOT NULL,
    updated_by      uuid,
    version         integer NOT NULL CHECK (version > 0),
    CONSTRAINT documents_tenant_id_key UNIQUE (tenant_id, id)
);
CREATE INDEX documents_owner_idx ON documents.documents (tenant_id, owner_type, owner_id, id DESC);
CREATE INDEX documents_retention_idx ON documents.documents (tenant_id, created_on, id);
CREATE UNIQUE INDEX documents_idempotency_idx ON documents.documents (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL;

CREATE TABLE documents.versions (
    id          uuid PRIMARY KEY,
    tenant_id   uuid NOT NULL,
    document_id uuid NOT NULL,
    number      integer NOT NULL CHECK (number > 0),
    storage_key text NOT NULL,
    mime_type   text NOT NULL CHECK (mime_type IN ('application/pdf', 'image/png', 'image/jpeg')),
    size_bytes  bigint NOT NULL CHECK (size_bytes > 0),
    sha256      bytea NOT NULL,
    scan_status text NOT NULL CHECK (scan_status IN ('pending', 'clean', 'infected')),
    scanned_at  timestamptz,
    created_at  timestamptz NOT NULL,
    created_by  uuid,
    CONSTRAINT versions_number_key UNIQUE (tenant_id, document_id, number),
    CONSTRAINT versions_document_fk FOREIGN KEY (tenant_id, document_id) REFERENCES documents.documents (tenant_id, id)
);
CREATE INDEX versions_pending_idx ON documents.versions (tenant_id, id) WHERE scan_status = 'pending';

ALTER TABLE documents.documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE documents.documents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON documents.documents USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE documents.versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE documents.versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON documents.versions USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- Sin DELETE: ningún documento se borra de forma automática.
GRANT SELECT, INSERT, UPDATE ON documents.documents TO seguros_app;
GRANT SELECT, INSERT, UPDATE ON documents.versions TO seguros_app;

-- Solo IDs de tenant, para el barrido del antivirus.
-- +goose StatementBegin
CREATE FUNCTION documents.tenants_with_pending_scans()
RETURNS TABLE (tenant_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT DISTINCT v.tenant_id FROM documents.versions v WHERE v.scan_status = 'pending'
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION documents.tenants_with_pending_scans() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION documents.tenants_with_pending_scans() TO seguros_worker;

-- +goose Down
DROP FUNCTION documents.tenants_with_pending_scans();
DROP TABLE documents.versions;
DROP TABLE documents.documents;
DROP SCHEMA documents;
