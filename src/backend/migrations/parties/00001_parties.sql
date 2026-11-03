-- Terceros de cada agencia (ROADMAP Fase 2). El número de documento es dato Restringido: solo se
-- guarda cifrado (document_ciphertext) con su blind index (document_bidx) para unicidad y búsqueda.
-- document_last4 se conserva en claro para mostrarlo enmascarado sin descifrar en cada listado.
-- name, email, phone, address y city son datos Confidenciales: nunca en logs ni en eventos.
-- +goose Up
CREATE SCHEMA parties;
GRANT USAGE ON SCHEMA parties TO seguros_app;

CREATE TABLE parties.parties (
    id                  uuid PRIMARY KEY,
    tenant_id           uuid NOT NULL,
    kind                text NOT NULL CHECK (kind IN ('person', 'organization')),
    name                text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 200),
    document_type       text NOT NULL CHECK (document_type IN ('CC', 'CE', 'PPT', 'NIT')),
    document_ciphertext bytea NOT NULL,
    document_bidx       bytea NOT NULL,
    document_last4      text NOT NULL CHECK (char_length(document_last4) <= 4),
    email               text CHECK (char_length(email) <= 254),
    phone               text CHECK (char_length(phone) <= 30),
    address             text CHECK (char_length(address) <= 200),
    city                text CHECK (char_length(city) <= 100),
    created_at          timestamptz NOT NULL,
    created_by          uuid,
    updated_at          timestamptz NOT NULL,
    updated_by          uuid,
    version             integer NOT NULL CHECK (version > 0),
    CONSTRAINT parties_kind_document_chk CHECK ((kind = 'organization') = (document_type = 'NIT')),
    CONSTRAINT parties_tenant_id_key UNIQUE (tenant_id, id),
    CONSTRAINT parties_document_key UNIQUE (tenant_id, document_type, document_bidx)
);
CREATE INDEX parties_tenant_keyset_idx ON parties.parties (tenant_id, id DESC);

ALTER TABLE parties.parties ENABLE ROW LEVEL SECURITY;
ALTER TABLE parties.parties FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON parties.parties
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
GRANT SELECT, INSERT, UPDATE ON parties.parties TO seguros_app;

-- Autorizaciones de tratamiento de datos (Ley 1581/2012). Append-only: una revocación es una fila nueva.
CREATE TABLE parties.consents (
    id           uuid PRIMARY KEY,
    tenant_id    uuid NOT NULL,
    party_id     uuid NOT NULL,
    status       text NOT NULL CHECK (status IN ('granted', 'revoked')),
    purpose      text NOT NULL CHECK (char_length(purpose) BETWEEN 3 AND 200),
    text_version text NOT NULL CHECK (char_length(text_version) BETWEEN 1 AND 40),
    consent_date date NOT NULL,
    recorded_at  timestamptz NOT NULL,
    recorded_by  uuid,
    CONSTRAINT consents_party_fk FOREIGN KEY (tenant_id, party_id) REFERENCES parties.parties (tenant_id, id)
);
CREATE INDEX consents_party_idx ON parties.consents (tenant_id, party_id, id DESC);

ALTER TABLE parties.consents ENABLE ROW LEVEL SECURITY;
ALTER TABLE parties.consents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON parties.consents
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
GRANT SELECT, INSERT ON parties.consents TO seguros_app;

-- +goose Down
DROP TABLE parties.consents;
DROP TABLE parties.parties;
DROP SCHEMA parties;
