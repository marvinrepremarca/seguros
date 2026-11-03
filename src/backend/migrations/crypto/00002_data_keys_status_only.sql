-- Auditoría de seguridad 1.10 (B1): la app solo puede retirar una DEK (active → retired). No puede
-- sobrescribir la llave envuelta ni reactivar una retirada: ambas cosas destruirían o
-- debilitarían los datos cifrados del tenant.
-- +goose Up
REVOKE UPDATE ON crypto.data_keys FROM seguros_app;
GRANT UPDATE (status) ON crypto.data_keys TO seguros_app;

-- +goose StatementBegin
CREATE FUNCTION crypto.only_retire() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF NEW.tenant_id <> OLD.tenant_id OR NEW.version <> OLD.version OR NEW.wrapped <> OLD.wrapped
       OR NEW.kek_ref <> OLD.kek_ref OR NEW.created_at <> OLD.created_at
       OR NOT (OLD.status = 'active' AND NEW.status = 'retired') THEN
        RAISE EXCEPTION 'crypto.data_keys: solo se permite retirar una DEK activa' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER data_keys_only_retire BEFORE UPDATE ON crypto.data_keys
    FOR EACH ROW EXECUTE FUNCTION crypto.only_retire();

-- +goose Down
DROP TRIGGER data_keys_only_retire ON crypto.data_keys;
DROP FUNCTION crypto.only_retire();
REVOKE UPDATE (status) ON crypto.data_keys FROM seguros_app;
GRANT UPDATE ON crypto.data_keys TO seguros_app;
