-- Recordatorios de cuota (DECISIONS T1): solo IDs de tenant, para el barrido del worker.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION billing.tenants_with_installments_due_on(p_dates date[])
RETURNS TABLE (tenant_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT DISTINCT i.tenant_id FROM billing.installments i
    WHERE i.status = 'pending' AND i.due_date = ANY (p_dates)
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION billing.tenants_with_installments_due_on(date[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION billing.tenants_with_installments_due_on(date[]) TO seguros_worker;

-- +goose Down
DROP FUNCTION billing.tenants_with_installments_due_on(date[]);
