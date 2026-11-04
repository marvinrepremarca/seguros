-- Reproceso de mensajes fallidos desde platformctl. Vuelve a publicar el evento: los consumidores que
-- ya lo procesaron lo ignoran por su inbox, y solo el que falló lo reintenta. Si el outbox ya se purgó,
-- se reconstruye desde el mensaje guardado. Solo seguros_platform puede ejecutarla.
-- +goose Up
ALTER TABLE messaging.dead_letters ADD COLUMN retried_at timestamptz;
GRANT USAGE ON SCHEMA messaging TO seguros_platform;
GRANT SELECT ON messaging.dead_letters TO seguros_platform;

-- +goose StatementBegin
CREATE FUNCTION messaging.retry_dead_letter(p_tenant uuid, p_id uuid, p_at timestamptz)
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
    INSERT INTO messaging.outbox (id, tenant_id, topic, payload, occurred_at)
    VALUES (d.event_id, d.tenant_id, d.topic, d.payload, d.failed_at)
    ON CONFLICT (id) DO UPDATE SET published_at = NULL, attempts = 0, last_error = NULL;
    RETURN true;
END;
$$;
-- +goose StatementEnd
REVOKE EXECUTE ON FUNCTION messaging.retry_dead_letter(uuid, uuid, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION messaging.retry_dead_letter(uuid, uuid, timestamptz) TO seguros_platform;

-- La función corre como dueño, con FORCE RLS: necesita ver y actualizar todas las filas.
CREATE POLICY owner_retry ON messaging.dead_letters TO CURRENT_USER USING (true) WITH CHECK (true);

-- +goose Down
DROP POLICY owner_retry ON messaging.dead_letters;
DROP FUNCTION messaging.retry_dead_letter(uuid, uuid, timestamptz);
REVOKE SELECT ON messaging.dead_letters FROM seguros_platform;
ALTER TABLE messaging.dead_letters DROP COLUMN retried_at;
