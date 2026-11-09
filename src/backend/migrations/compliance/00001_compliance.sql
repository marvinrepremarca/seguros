-- Cumplimiento (Flujo 10): KYC con consulta manual, PQR y solicitudes de habeas data.
-- +goose Up
CREATE SCHEMA compliance;
GRANT USAGE ON SCHEMA compliance TO seguros_app;

-- Append-only: una corrección es una verificación nueva. Vale la más reciente del tercero, y solo
-- mientras su documento sea el verificado (ADR 0012).
CREATE TABLE compliance.kyc_checks (
    id                 uuid PRIMARY KEY,
    tenant_id          uuid NOT NULL,
    party_id           uuid NOT NULL,
    document_bidx      bytea NOT NULL,
    result             text NOT NULL CHECK (result IN ('approved', 'rejected')),
    source             text NOT NULL CHECK (char_length(source) BETWEEN 2 AND 200),
    checked_on         date NOT NULL,
    details_ciphertext bytea,
    created_at         timestamptz NOT NULL,
    created_by         uuid
);
CREATE INDEX kyc_checks_party_idx ON compliance.kyc_checks (tenant_id, party_id, id DESC);

CREATE TABLE compliance.complaints (
    id          uuid PRIMARY KEY,
    tenant_id   uuid NOT NULL,
    party_id    uuid,
    channel     text NOT NULL CHECK (char_length(channel) BETWEEN 2 AND 60),
    subject     text NOT NULL CHECK (char_length(subject) BETWEEN 3 AND 200),
    description text NOT NULL CHECK (char_length(description) BETWEEN 3 AND 4000),
    status      text NOT NULL CHECK (status IN ('received', 'in_progress', 'resolved')),
    resolution  text CHECK (char_length(resolution) <= 4000),
    received_on date NOT NULL,
    created_at  timestamptz NOT NULL,
    created_by  uuid,
    updated_at  timestamptz NOT NULL,
    updated_by  uuid,
    version     integer NOT NULL CHECK (version > 0),
    CONSTRAINT complaints_resolved_has_answer CHECK (status <> 'resolved' OR resolution IS NOT NULL)
);
CREATE INDEX complaints_tenant_keyset_idx ON compliance.complaints (tenant_id, id DESC);

-- La supresión se registra y se decide a mano: ningún dato se borra de forma automática.
CREATE TABLE compliance.suppression_requests (
    id         uuid PRIMARY KEY,
    tenant_id  uuid NOT NULL,
    party_id   uuid NOT NULL,
    status     text NOT NULL CHECK (status IN ('requested', 'accepted', 'denied')),
    resolution text CHECK (char_length(resolution) <= 2000),
    created_at timestamptz NOT NULL,
    created_by uuid,
    updated_at timestamptz NOT NULL,
    updated_by uuid,
    version    integer NOT NULL CHECK (version > 0)
);
CREATE INDEX suppression_tenant_keyset_idx ON compliance.suppression_requests (tenant_id, id DESC);

ALTER TABLE compliance.kyc_checks ENABLE ROW LEVEL SECURITY;
ALTER TABLE compliance.kyc_checks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON compliance.kyc_checks USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE compliance.complaints ENABLE ROW LEVEL SECURITY;
ALTER TABLE compliance.complaints FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON compliance.complaints USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE compliance.suppression_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE compliance.suppression_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON compliance.suppression_requests USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

GRANT SELECT, INSERT ON compliance.kyc_checks TO seguros_app;
GRANT SELECT, INSERT, UPDATE ON compliance.complaints TO seguros_app;
GRANT SELECT, INSERT, UPDATE ON compliance.suppression_requests TO seguros_app;

-- +goose Down
DROP TABLE compliance.suppression_requests;
DROP TABLE compliance.complaints;
DROP TABLE compliance.kyc_checks;
DROP SCHEMA compliance;
