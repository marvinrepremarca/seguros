-- Bitácora inmutable de acciones (ROADMAP 1.9, ARCHITECTURE.md §4.4). Append-only: el rol de la
-- app solo tiene INSERT y SELECT (bajo RLS) y un trigger rechaza UPDATE/DELETE incluso al dueño.
-- Particionada por mes sobre occurred_at; las particiones futuras las crea el job periódico
-- audit-partitions (River) mediante audit.ensure_partitions (SECURITY DEFINER).
-- Nunca contiene PII ni datos Restringidos: details solo lleva IDs, códigos y valores técnicos.
-- +goose Up
CREATE SCHEMA audit;
GRANT USAGE ON SCHEMA audit TO seguros_app;

CREATE TABLE audit.events (
    id            uuid        NOT NULL,
    tenant_id     uuid        NOT NULL,
    occurred_at   timestamptz NOT NULL,
    actor_type    text        NOT NULL CHECK (actor_type IN ('user', 'system')),
    actor_id      uuid,
    action        text        NOT NULL CHECK (action ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*){2,4}$' AND char_length(action) <= 120),
    resource_type text        NOT NULL CHECK (resource_type ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*){0,3}$' AND char_length(resource_type) <= 80),
    resource_id   text        CHECK (char_length(resource_id) <= 200),
    outcome       text        NOT NULL CHECK (outcome IN ('success', 'denied', 'failure')),
    request_id    text        CHECK (char_length(request_id) <= 64),
    trace_id      text        CHECK (trace_id ~ '^[0-9a-f]{32}$'),
    details       jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(details) = 'object' AND pg_column_size(details) <= 8192),
    CONSTRAINT events_actor_chk CHECK ((actor_type = 'user') = (actor_id IS NOT NULL)),
    PRIMARY KEY (tenant_id, occurred_at, id)
) PARTITION BY RANGE (occurred_at);

-- Red de seguridad: si faltara la partición del mes, el evento no se pierde.
CREATE TABLE audit.events_default PARTITION OF audit.events DEFAULT;

ALTER TABLE audit.events ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON audit.events
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

GRANT SELECT, INSERT ON audit.events TO seguros_app;

CREATE INDEX events_tenant_resource_idx ON audit.events (tenant_id, resource_type, resource_id, occurred_at);

-- +goose StatementBegin
CREATE FUNCTION audit.forbid_mutation() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    RAISE EXCEPTION 'audit.events es append-only' USING ERRCODE = 'insufficient_privilege';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER events_append_only BEFORE UPDATE OR DELETE ON audit.events
    FOR EACH ROW EXECUTE FUNCTION audit.forbid_mutation();

-- Crea (si no existen) las particiones mensuales desde el mes de p_from hasta p_months meses
-- después. Idempotente. La ejecuta el rol de la app desde el job periódico; corre como dueño.
-- +goose StatementBegin
CREATE FUNCTION audit.ensure_partitions(p_from date, p_months integer)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    m       date;
    created integer := 0;
    part    text;
BEGIN
    IF p_months < 0 OR p_months > 24 THEN
        RAISE EXCEPTION 'p_months fuera de rango (0..24)' USING ERRCODE = 'invalid_parameter_value';
    END IF;
    FOR i IN 0..p_months LOOP
        m := (date_trunc('month', p_from) + make_interval(months => i))::date;
        part := 'events_y' || to_char(m, 'YYYY') || 'm' || to_char(m, 'MM');
        IF to_regclass('audit.' || part) IS NULL THEN
            EXECUTE format(
                'CREATE TABLE audit.%I PARTITION OF audit.events FOR VALUES FROM (%L) TO (%L)',
                part, m::timestamp AT TIME ZONE 'UTC', (m + interval '1 month')::timestamp AT TIME ZONE 'UTC');
            created := created + 1;
        END IF;
    END LOOP;
    RETURN created;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION audit.ensure_partitions(date, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION audit.ensure_partitions(date, integer) TO seguros_app;

SELECT audit.ensure_partitions(current_date, 12);

-- +goose Down
DROP FUNCTION audit.ensure_partitions(date, integer);
DROP TABLE audit.events;
DROP FUNCTION audit.forbid_mutation();
DROP SCHEMA audit;
