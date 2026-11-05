-- Previos de la Fase 3 (ROADMAP):
-- 1. El reproceso se dirige solo a los consumidores que fallaron. Republicar a todos haría que un
--    consumidor apagado cuando ocurrió el evento y encendido después lo procesara tarde.
-- 2. Limpieza de messaging.inbox.
-- 3. Lectura de la salud de la mensajería, sin contenido de mensajes, para métricas y alertas.
-- +goose Up
ALTER TABLE messaging.outbox ADD COLUMN target_consumers text[];

DROP FUNCTION messaging.claim_outbox(integer);
-- +goose StatementBegin
CREATE FUNCTION messaging.claim_outbox(p_limit integer)
RETURNS TABLE (id uuid, tenant_id uuid, topic text, payload jsonb, occurred_at timestamptz, attempts integer, target_consumers text[])
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT o.id, o.tenant_id, o.topic, o.payload, o.occurred_at, o.attempts, o.target_consumers
    FROM messaging.outbox o
    WHERE o.published_at IS NULL AND o.attempts < 25
    ORDER BY o.attempts, o.occurred_at, o.id
    LIMIT least(greatest(p_limit, 1), 500)
    FOR UPDATE SKIP LOCKED
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION messaging.claim_outbox(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION messaging.claim_outbox(integer) TO seguros_worker;

-- Si la fila sigue pendiente, conserva a quienes ya esperaban el evento: NULL significa todos, y dos
-- consumidores que fallaron con el mismo evento se pueden reprocesar antes de que el relay publique.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION messaging.retry_dead_letter(p_tenant uuid, p_id uuid, p_at timestamptz)
RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE d messaging.dead_letters%ROWTYPE;
BEGIN
    UPDATE messaging.dead_letters SET retried_at = p_at
    WHERE id = p_id AND tenant_id = p_tenant AND retried_at IS NULL
    RETURNING * INTO d;
    IF NOT FOUND THEN
        RETURN false;
    END IF;
    INSERT INTO messaging.outbox AS o (id, tenant_id, topic, payload, occurred_at, target_consumers)
    VALUES (d.event_id, d.tenant_id, d.topic, d.payload, coalesce(d.occurred_at, d.failed_at), ARRAY[d.consumer])
    ON CONFLICT (id) DO UPDATE SET
        published_at = NULL, attempts = 0, last_error = NULL,
        target_consumers = CASE
            WHEN o.published_at IS NOT NULL THEN ARRAY[d.consumer]
            WHEN o.target_consumers IS NULL THEN NULL
            ELSE array_append(o.target_consumers, d.consumer)
        END
    WHERE o.tenant_id = EXCLUDED.tenant_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'evento de otro tenant';
    END IF;
    RETURN true;
END;
$$;
-- +goose StatementEnd

CREATE INDEX inbox_processed_at_idx ON messaging.inbox (processed_at);
-- La función corre como dueño, con FORCE RLS: necesita ver y borrar las filas de todos los tenants.
CREATE POLICY owner_purge ON messaging.inbox TO CURRENT_USER USING (true) WITH CHECK (true);

-- +goose StatementBegin
CREATE FUNCTION messaging.purge_inbox(p_before timestamptz)
RETURNS integer
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    WITH d AS (DELETE FROM messaging.inbox WHERE processed_at < p_before RETURNING 1)
    SELECT count(*)::integer FROM d
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION messaging.purge_inbox(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION messaging.purge_inbox(timestamptz) TO seguros_worker;

-- +goose StatementBegin
CREATE FUNCTION messaging.health(p_at timestamptz)
RETURNS TABLE (dead_letters_pending bigint, outbox_stuck bigint, outbox_oldest_pending_seconds bigint)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT
        (SELECT count(*) FROM messaging.dead_letters WHERE retried_at IS NULL),
        (SELECT count(*) FROM messaging.outbox WHERE published_at IS NULL AND attempts >= 25),
        (SELECT coalesce(extract(epoch FROM p_at - min(occurred_at))::bigint, 0)
         FROM messaging.outbox WHERE published_at IS NULL)
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION messaging.health(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION messaging.health(timestamptz) TO seguros_worker;

-- +goose Down
DROP FUNCTION messaging.health(timestamptz);
DROP FUNCTION messaging.purge_inbox(timestamptz);
DROP POLICY owner_purge ON messaging.inbox;
DROP INDEX messaging.inbox_processed_at_idx;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION messaging.retry_dead_letter(p_tenant uuid, p_id uuid, p_at timestamptz)
RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE d messaging.dead_letters%ROWTYPE;
BEGIN
    UPDATE messaging.dead_letters SET retried_at = p_at
    WHERE id = p_id AND tenant_id = p_tenant AND retried_at IS NULL
    RETURNING * INTO d;
    IF NOT FOUND THEN
        RETURN false;
    END IF;
    INSERT INTO messaging.outbox AS o (id, tenant_id, topic, payload, occurred_at)
    VALUES (d.event_id, d.tenant_id, d.topic, d.payload, coalesce(d.occurred_at, d.failed_at))
    ON CONFLICT (id) DO UPDATE SET published_at = NULL, attempts = 0, last_error = NULL
    WHERE o.tenant_id = EXCLUDED.tenant_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'evento de otro tenant';
    END IF;
    RETURN true;
END;
$$;
-- +goose StatementEnd

DROP FUNCTION messaging.claim_outbox(integer);
-- +goose StatementBegin
CREATE FUNCTION messaging.claim_outbox(p_limit integer)
RETURNS TABLE (id uuid, tenant_id uuid, topic text, payload jsonb, occurred_at timestamptz, attempts integer)
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT o.id, o.tenant_id, o.topic, o.payload, o.occurred_at, o.attempts
    FROM messaging.outbox o
    WHERE o.published_at IS NULL AND o.attempts < 25
    ORDER BY o.attempts, o.occurred_at, o.id
    LIMIT least(greatest(p_limit, 1), 500)
    FOR UPDATE SKIP LOCKED
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION messaging.claim_outbox(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION messaging.claim_outbox(integer) TO seguros_worker;

ALTER TABLE messaging.outbox DROP COLUMN target_consumers;
