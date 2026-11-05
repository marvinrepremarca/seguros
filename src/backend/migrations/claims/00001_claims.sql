-- Siniestros (Flujo 7). El detalle de salud va cifrado y ligado al ID del siniestro (DECISIONS N30).
-- +goose Up
CREATE SCHEMA claims;
GRANT USAGE ON SCHEMA claims TO seguros_app;

CREATE TABLE claims.claims (
    id                    uuid PRIMARY KEY,
    tenant_id             uuid NOT NULL,
    policy_id             uuid NOT NULL,
    policy_number         text,
    insurer_id            uuid NOT NULL,
    holder_party_id       uuid NOT NULL,
    occurred_on           date NOT NULL,
    location              text NOT NULL CHECK (char_length(location) BETWEEN 2 AND 200),
    claim_type            text NOT NULL CHECK (char_length(claim_type) BETWEEN 2 AND 100),
    description           text NOT NULL CHECK (char_length(description) BETWEEN 3 AND 2000),
    currency              text NOT NULL,
    claimed_amount        numeric(19, 4) CHECK (claimed_amount >= 0),
    paid_amount           numeric(19, 4) CHECK (paid_amount >= 0),
    status                text NOT NULL CHECK (status IN ('reported', 'notified', 'documenting', 'evaluating', 'approved', 'objected', 'paid', 'closed')),
    insurer_claim_number  text CHECK (char_length(insurer_claim_number) <= 60),
    decision_support      text CHECK (char_length(decision_support) <= 200),
    alert_out_of_coverage boolean NOT NULL,
    alert_in_arrears      boolean NOT NULL,
    alert_not_in_force    boolean NOT NULL,
    health_ciphertext     bytea,
    reported_at           timestamptz NOT NULL,
    reported_by           uuid,
    notified_at           timestamptz,
    updated_at            timestamptz NOT NULL,
    updated_by            uuid,
    version               integer NOT NULL CHECK (version > 0),
    CONSTRAINT claims_tenant_id_key UNIQUE (tenant_id, id)
);
CREATE INDEX claims_tenant_keyset_idx ON claims.claims (tenant_id, id DESC);
CREATE INDEX claims_policy_idx ON claims.claims (tenant_id, policy_id, id DESC);

-- Lista de control de soportes hasta que exista Documents (DECISIONS N30).
CREATE TABLE claims.checklist_items (
    id         uuid PRIMARY KEY,
    tenant_id  uuid NOT NULL,
    claim_id   uuid NOT NULL,
    label      text NOT NULL CHECK (char_length(label) BETWEEN 2 AND 200),
    received   boolean NOT NULL,
    updated_at timestamptz NOT NULL,
    updated_by uuid,
    CONSTRAINT checklist_claim_fk FOREIGN KEY (tenant_id, claim_id) REFERENCES claims.claims (tenant_id, id)
);
CREATE INDEX checklist_claim_idx ON claims.checklist_items (tenant_id, claim_id, id);

-- Gestiones con la aseguradora: append-only.
CREATE TABLE claims.notes (
    id         uuid PRIMARY KEY,
    tenant_id  uuid NOT NULL,
    claim_id   uuid NOT NULL,
    body       text NOT NULL CHECK (char_length(body) BETWEEN 3 AND 2000),
    created_at timestamptz NOT NULL,
    created_by uuid,
    CONSTRAINT notes_claim_fk FOREIGN KEY (tenant_id, claim_id) REFERENCES claims.claims (tenant_id, id)
);
CREATE INDEX notes_claim_idx ON claims.notes (tenant_id, claim_id, id);

ALTER TABLE claims.claims ENABLE ROW LEVEL SECURITY;
ALTER TABLE claims.claims FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON claims.claims USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE claims.checklist_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE claims.checklist_items FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON claims.checklist_items USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE claims.notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE claims.notes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON claims.notes USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

GRANT SELECT, INSERT, UPDATE ON claims.claims TO seguros_app;
GRANT SELECT, INSERT, UPDATE ON claims.checklist_items TO seguros_app;
GRANT SELECT, INSERT ON claims.notes TO seguros_app;

-- +goose Down
DROP TABLE claims.notes;
DROP TABLE claims.checklist_items;
DROP TABLE claims.claims;
DROP SCHEMA claims;
