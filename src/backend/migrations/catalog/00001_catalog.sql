-- Catálogo de cada agencia (ROADMAP Fase 2): aseguradoras, ramos, productos y acuerdos de comisión.
-- Los registros se desactivan, no se borran: pólizas históricas los referencian.
-- +goose Up
CREATE SCHEMA catalog;
GRANT USAGE ON SCHEMA catalog TO seguros_app;
-- btree_gist permite la restricción de exclusión con igualdad sobre uuid + solape de vigencias.
CREATE EXTENSION IF NOT EXISTS btree_gist SCHEMA catalog;

CREATE TABLE catalog.insurers (
    id         uuid PRIMARY KEY,
    tenant_id  uuid NOT NULL,
    name       text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 120),
    active     boolean NOT NULL,
    created_at timestamptz NOT NULL,
    created_by uuid,
    updated_at timestamptz NOT NULL,
    updated_by uuid,
    version    integer NOT NULL CHECK (version > 0),
    CONSTRAINT insurers_tenant_id_key UNIQUE (tenant_id, id)
);
CREATE UNIQUE INDEX insurers_tenant_name_key ON catalog.insurers (tenant_id, lower(name));

CREATE TABLE catalog.lines_of_business (
    id              uuid PRIMARY KEY,
    tenant_id       uuid NOT NULL,
    name            text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 120),
    risk_schema_key text NOT NULL CHECK (risk_schema_key IN ('auto', 'life', 'home', 'business', 'generic')),
    active          boolean NOT NULL,
    created_at      timestamptz NOT NULL,
    created_by      uuid,
    updated_at      timestamptz NOT NULL,
    updated_by      uuid,
    version         integer NOT NULL CHECK (version > 0),
    CONSTRAINT lines_tenant_id_key UNIQUE (tenant_id, id)
);
CREATE UNIQUE INDEX lines_tenant_name_key ON catalog.lines_of_business (tenant_id, lower(name));

CREATE TABLE catalog.products (
    id                  uuid PRIMARY KEY,
    tenant_id           uuid NOT NULL,
    insurer_id          uuid NOT NULL,
    line_of_business_id uuid NOT NULL,
    name                text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 120),
    active              boolean NOT NULL,
    created_at          timestamptz NOT NULL,
    created_by          uuid,
    updated_at          timestamptz NOT NULL,
    updated_by          uuid,
    version             integer NOT NULL CHECK (version > 0),
    CONSTRAINT products_tenant_id_key UNIQUE (tenant_id, id),
    CONSTRAINT products_insurer_fk FOREIGN KEY (tenant_id, insurer_id) REFERENCES catalog.insurers (tenant_id, id),
    CONSTRAINT products_line_fk FOREIGN KEY (tenant_id, line_of_business_id) REFERENCES catalog.lines_of_business (tenant_id, id)
);
CREATE UNIQUE INDEX products_tenant_name_key ON catalog.products (tenant_id, insurer_id, line_of_business_id, lower(name));

CREATE TABLE catalog.commission_agreements (
    id                  uuid PRIMARY KEY,
    tenant_id           uuid NOT NULL,
    insurer_id          uuid NOT NULL,
    line_of_business_id uuid NOT NULL,
    product_id          uuid,
    basis               text NOT NULL CHECK (basis IN ('paid_premium', 'written_premium')),
    percentage          numeric(9,6) NOT NULL CHECK (percentage >= 0 AND percentage <= 100),
    valid_from          date NOT NULL,
    valid_to            date NOT NULL,
    created_at          timestamptz NOT NULL,
    created_by          uuid,
    updated_at          timestamptz NOT NULL,
    updated_by          uuid,
    version             integer NOT NULL CHECK (version > 0),
    CONSTRAINT agreements_validity_chk CHECK (valid_to > valid_from),
    CONSTRAINT agreements_insurer_fk FOREIGN KEY (tenant_id, insurer_id) REFERENCES catalog.insurers (tenant_id, id),
    CONSTRAINT agreements_line_fk FOREIGN KEY (tenant_id, line_of_business_id) REFERENCES catalog.lines_of_business (tenant_id, id),
    CONSTRAINT agreements_product_fk FOREIGN KEY (tenant_id, product_id) REFERENCES catalog.products (tenant_id, id),
    -- Dos acuerdos del mismo ámbito no se solapan en vigencia (la cobertura de las fechas es inclusiva).
    CONSTRAINT agreements_no_overlap EXCLUDE USING gist (
        tenant_id WITH =,
        insurer_id WITH =,
        line_of_business_id WITH =,
        (coalesce(product_id, '00000000-0000-0000-0000-000000000000'::uuid)) WITH =,
        daterange(valid_from, valid_to, '[]') WITH &&
    )
);
CREATE INDEX agreements_tenant_keyset_idx ON catalog.commission_agreements (tenant_id, id DESC);

ALTER TABLE catalog.insurers ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalog.insurers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON catalog.insurers
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

ALTER TABLE catalog.lines_of_business ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalog.lines_of_business FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON catalog.lines_of_business
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

ALTER TABLE catalog.products ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalog.products FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON catalog.products
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

ALTER TABLE catalog.commission_agreements ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalog.commission_agreements FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON catalog.commission_agreements
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

GRANT SELECT, INSERT, UPDATE ON catalog.insurers, catalog.lines_of_business, catalog.products, catalog.commission_agreements TO seguros_app;

-- +goose Down
DROP TABLE catalog.commission_agreements;
DROP TABLE catalog.products;
DROP TABLE catalog.lines_of_business;
DROP TABLE catalog.insurers;
DROP EXTENSION IF EXISTS btree_gist;
DROP SCHEMA catalog;
