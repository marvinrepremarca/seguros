-- DECISIONS N11: la cartera es de la agencia. El alcance del asesor sale del responsable vigente, no
-- de quién registró al cliente; created_by queda solo como auditoría. Se parte de quien lo registró:
-- no hay datos de producción, y un responsable que no sea asesor no da acceso adicional.
-- +goose Up
ALTER TABLE parties.parties ADD COLUMN responsible_agent_id uuid;
-- FORCE RLS también aplica al dueño: sin esto el relleno falla por falta de app.tenant_id.
ALTER TABLE parties.parties NO FORCE ROW LEVEL SECURITY;
UPDATE parties.parties SET responsible_agent_id = created_by;
ALTER TABLE parties.parties FORCE ROW LEVEL SECURITY;
CREATE INDEX parties_tenant_responsible_idx ON parties.parties (tenant_id, responsible_agent_id);
DROP INDEX parties.parties_tenant_created_by_idx;

-- +goose Down
CREATE INDEX parties_tenant_created_by_idx ON parties.parties (tenant_id, created_by);
DROP INDEX parties.parties_tenant_responsible_idx;
ALTER TABLE parties.parties DROP COLUMN responsible_agent_id;
