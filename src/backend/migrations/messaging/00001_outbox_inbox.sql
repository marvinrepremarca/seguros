-- Mensajería entre módulos (ARCHITECTURE.md §5): outbox transaccional, inbox idempotente por
-- consumidor y mensajes fallidos. Los eventos transportan IDs, nunca datos Restringidos.
--
-- outbox: el rol de la app solo INSERTA, y solo para su tenant (RLS WITH CHECK), dentro de la
-- transacción del cambio. El relay (cmd/worker) lee entre tenants únicamente mediante las
-- funciones SECURITY DEFINER de abajo, que solo exponen lo necesario para publicar.
-- +goose Up
CREATE SCHEMA messaging;
GRANT USAGE ON SCHEMA messaging TO seguros_app;

CREATE TABLE messaging.outbox (
    id           uuid        PRIMARY KEY,
    tenant_id    uuid        NOT NULL,
    topic        text        NOT NULL CHECK (topic ~ '^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*$' AND char_length(topic) <= 120),
    payload      jsonb       NOT NULL CHECK (jsonb_typeof(payload) = 'object' AND pg_column_size(payload) <= 16384),
    occurred_at  timestamptz NOT NULL,
    published_at timestamptz,
    attempts     integer     NOT NULL DEFAULT 0,
    last_error   text        CHECK (char_length(last_error) <= 500)
);
CREATE INDEX outbox_pending_idx ON messaging.outbox (occurred_at) WHERE published_at IS NULL;

ALTER TABLE messaging.outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE messaging.outbox FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON messaging.outbox
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
-- Las funciones del relay corren como dueño: necesitan ver y actualizar todas las filas.
CREATE POLICY owner_relay ON messaging.outbox TO CURRENT_USER USING (true) WITH CHECK (true);

GRANT INSERT ON messaging.outbox TO seguros_app;

-- Reclama hasta p_limit mensajes pendientes (FOR UPDATE SKIP LOCKED: varios relays no publican
-- el mismo). Los bloqueos duran hasta el fin de la transacción del relay.
-- +goose StatementBegin
CREATE FUNCTION messaging.claim_outbox(p_limit integer)
RETURNS TABLE (id uuid, tenant_id uuid, topic text, payload jsonb, occurred_at timestamptz, attempts integer)
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT o.id, o.tenant_id, o.topic, o.payload, o.occurred_at, o.attempts
    FROM messaging.outbox o
    WHERE o.published_at IS NULL
    ORDER BY o.occurred_at, o.id
    LIMIT least(greatest(p_limit, 1), 500)
    FOR UPDATE SKIP LOCKED
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION messaging.mark_published(p_ids uuid[], p_at timestamptz)
RETURNS integer
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    WITH u AS (
        UPDATE messaging.outbox SET published_at = p_at, last_error = NULL
        WHERE id = ANY (p_ids) AND published_at IS NULL
        RETURNING 1
    )
    SELECT count(*)::integer FROM u
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION messaging.mark_failed(p_id uuid, p_error text)
RETURNS void
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    UPDATE messaging.outbox SET attempts = attempts + 1, last_error = left(p_error, 500)
    WHERE id = p_id AND published_at IS NULL
$$;
-- +goose StatementEnd

-- Borra mensajes ya publicados antes de p_before (job periódico de limpieza).
-- +goose StatementBegin
CREATE FUNCTION messaging.purge_published(p_before timestamptz)
RETURNS integer
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    WITH d AS (
        DELETE FROM messaging.outbox WHERE published_at IS NOT NULL AND published_at < p_before
        RETURNING 1
    )
    SELECT count(*)::integer FROM d
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION messaging.claim_outbox(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION messaging.mark_published(uuid[], timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION messaging.mark_failed(uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION messaging.purge_published(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION messaging.claim_outbox(integer) TO seguros_app;
GRANT EXECUTE ON FUNCTION messaging.mark_published(uuid[], timestamptz) TO seguros_app;
GRANT EXECUTE ON FUNCTION messaging.mark_failed(uuid, text) TO seguros_app;
GRANT EXECUTE ON FUNCTION messaging.purge_published(timestamptz) TO seguros_app;

-- inbox: un evento se procesa como máximo una vez por consumidor (clave = id del evento). La
-- fila se inserta en la misma transacción que los efectos del consumidor.
CREATE TABLE messaging.inbox (
    consumer     text        NOT NULL CHECK (consumer ~ '^[a-z][a-z0-9]*\.[a-z][a-z0-9-]*$' AND char_length(consumer) <= 120),
    event_id     uuid        NOT NULL,
    tenant_id    uuid        NOT NULL,
    processed_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, consumer, event_id)
);

ALTER TABLE messaging.inbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE messaging.inbox FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON messaging.inbox
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

GRANT SELECT, INSERT ON messaging.inbox TO seguros_app;

-- Mensajes que un consumidor no pudo procesar tras sus reintentos (para revisión y reproceso).
CREATE TABLE messaging.dead_letters (
    id        uuid        PRIMARY KEY,
    tenant_id uuid        NOT NULL,
    consumer  text        NOT NULL CHECK (char_length(consumer) <= 120),
    event_id  uuid        NOT NULL,
    topic     text        NOT NULL CHECK (char_length(topic) <= 120),
    payload   jsonb       NOT NULL,
    error     text        NOT NULL CHECK (char_length(error) <= 500),
    failed_at timestamptz NOT NULL
);
CREATE INDEX dead_letters_tenant_idx ON messaging.dead_letters (tenant_id, failed_at);

ALTER TABLE messaging.dead_letters ENABLE ROW LEVEL SECURITY;
ALTER TABLE messaging.dead_letters FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON messaging.dead_letters
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

GRANT SELECT, INSERT ON messaging.dead_letters TO seguros_app;

-- +goose Down
DROP TABLE messaging.dead_letters;
DROP TABLE messaging.inbox;
DROP FUNCTION messaging.purge_published(timestamptz);
DROP FUNCTION messaging.mark_failed(uuid, text);
DROP FUNCTION messaging.mark_published(uuid[], timestamptz);
DROP FUNCTION messaging.claim_outbox(integer);
DROP TABLE messaging.outbox;
DROP SCHEMA messaging;
