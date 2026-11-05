-- name: InsertLead :exec
INSERT INTO crm.leads (id, tenant_id, name, email, phone, source, line_of_business_id, agent_id, status, assigned_at, created_at, created_by, updated_at, updated_by, version)
VALUES (@id, @tenant_id, @name, @email, @phone, @source, @line_of_business_id, @agent_id, 'new', @assigned_at, @at, @actor, @at, @actor, 1);

-- name: GetLead :one
SELECT id, name, email, phone, source, line_of_business_id, agent_id, party_id, status, lost_reason, follow_up_on, contacted_at, assigned_at, version
FROM crm.leads WHERE tenant_id = @tenant_id AND id = @id;

-- name: UpdateLead :execrows
UPDATE crm.leads SET agent_id = @agent_id, party_id = @party_id, status = @status, lost_reason = @lost_reason, follow_up_on = @follow_up_on,
    contacted_at = @contacted_at, assigned_at = @assigned_at, updated_at = @at, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: ListLeads :many
SELECT id, name, email, phone, source, line_of_business_id, agent_id, party_id, status, lost_reason, follow_up_on, contacted_at, assigned_at, version
FROM crm.leads
WHERE tenant_id = @tenant_id
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('agent')::uuid IS NULL OR agent_id = sqlc.narg('agent')::uuid)
  AND (sqlc.narg('before')::uuid IS NULL OR id < sqlc.narg('before')::uuid)
ORDER BY id DESC LIMIT @page_size;

-- name: TransferLeads :execrows
-- Traspaso de cartera (DECISIONS N11): no mueve lo asignado después del corte ni lo terminado.
UPDATE crm.leads SET agent_id = @to_agent, assigned_at = @at, updated_at = @at, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND agent_id = @from_agent AND status NOT IN ('won', 'lost') AND assigned_at <= @requested_at;
