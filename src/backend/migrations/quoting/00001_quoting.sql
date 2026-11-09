-- Cotización (Flujo 2): solicitudes, ofertas de aseguradoras y parámetros.
-- +goose Up
CREATE SCHEMA quoting;
GRANT USAGE ON SCHEMA quoting TO seguros_app;

CREATE TABLE quoting.requests (
    id                  uuid PRIMARY KEY,
    tenant_id           uuid NOT NULL,
    lead_id             uuid,
    holder_party_id     uuid NOT NULL,
    line_of_business_id uuid NOT NULL,
    agent_id            uuid NOT NULL,
    risk_description    text NOT NULL CHECK (char_length(risk_description) BETWEEN 2 AND 200),
    risk_data           jsonb NOT NULL,
    status              text NOT NULL CHECK (status IN ('open', 'accepted', 'declined')),
    decline_reason      text CHECK (char_length(decline_reason) <= 500),
    policy_id           uuid,
    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL,
    updated_by          uuid,
    version             integer NOT NULL CHECK (version > 0),
    CONSTRAINT requests_tenant_id_key UNIQUE (tenant_id, id),
    CONSTRAINT requests_risk_size CHECK (pg_column_size(risk_data) <= 8192)
);
CREATE INDEX requests_tenant_keyset_idx ON quoting.requests (tenant_id, id DESC);
CREATE INDEX requests_agent_idx ON quoting.requests (tenant_id, agent_id, id DESC);

CREATE TABLE quoting.offers (
    id             uuid PRIMARY KEY,
    tenant_id      uuid NOT NULL,
    request_id     uuid NOT NULL,
    insurer_id     uuid NOT NULL,
    product_id     uuid,
    reference      text CHECK (char_length(reference) <= 60),
    currency       text NOT NULL,
    net_premium    numeric(19, 4) NOT NULL CHECK (net_premium >= 0),
    taxes          numeric(19, 4) NOT NULL CHECK (taxes >= 0),
    effective_from date NOT NULL,
    effective_to   date NOT NULL,
    valid_until    date NOT NULL,
    status         text NOT NULL CHECK (status IN ('offered', 'accepted', 'discarded')),
    risk_schema_key text NOT NULL,
    risk_schema_version integer NOT NULL,
    created_at     timestamptz NOT NULL,
    created_by     uuid,
    CONSTRAINT offers_tenant_id_key UNIQUE (tenant_id, id),
    CONSTRAINT offers_period_check CHECK (effective_to > effective_from),
    CONSTRAINT offers_request_fk FOREIGN KEY (tenant_id, request_id) REFERENCES quoting.requests (tenant_id, id)
);
CREATE INDEX offers_request_idx ON quoting.offers (tenant_id, request_id, id);
-- Una sola oferta aceptada por solicitud (DECISIONS N28).
CREATE UNIQUE INDEX offers_one_accepted_idx ON quoting.offers (tenant_id, request_id) WHERE status = 'accepted';

CREATE TABLE quoting.offer_coverages (
    tenant_id   uuid NOT NULL,
    offer_id    uuid NOT NULL,
    position    integer NOT NULL,
    name        text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 100),
    sum_insured numeric(19, 4) NOT NULL CHECK (sum_insured >= 0),
    deductible  text CHECK (char_length(deductible) <= 100),
    PRIMARY KEY (tenant_id, offer_id, position),
    CONSTRAINT offer_coverages_offer_fk FOREIGN KEY (tenant_id, offer_id) REFERENCES quoting.offers (tenant_id, id)
);

-- Días de vigencia de una oferta (DECISIONS T3).
CREATE TABLE quoting.settings (
    tenant_id        uuid PRIMARY KEY,
    offer_valid_days integer NOT NULL CHECK (offer_valid_days BETWEEN 1 AND 365),
    updated_at       timestamptz NOT NULL,
    updated_by       uuid
);

ALTER TABLE quoting.requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE quoting.requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON quoting.requests USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE quoting.offers ENABLE ROW LEVEL SECURITY;
ALTER TABLE quoting.offers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON quoting.offers USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE quoting.offer_coverages ENABLE ROW LEVEL SECURITY;
ALTER TABLE quoting.offer_coverages FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON quoting.offer_coverages USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE quoting.settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE quoting.settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON quoting.settings USING (tenant_id = current_setting('app.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

GRANT SELECT, INSERT, UPDATE ON quoting.requests TO seguros_app;
GRANT SELECT, INSERT, UPDATE ON quoting.offers TO seguros_app;
GRANT SELECT, INSERT ON quoting.offer_coverages TO seguros_app;
GRANT SELECT, INSERT, UPDATE ON quoting.settings TO seguros_app;

-- +goose Down
DROP TABLE quoting.settings;
DROP TABLE quoting.offer_coverages;
DROP TABLE quoting.offers;
DROP TABLE quoting.requests;
DROP SCHEMA quoting;
