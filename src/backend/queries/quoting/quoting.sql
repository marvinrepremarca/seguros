-- name: InsertRequest :exec
INSERT INTO quoting.requests (id, tenant_id, lead_id, holder_party_id, line_of_business_id, agent_id, risk_description, risk_data, status, created_at, updated_at, updated_by, version)
VALUES (@id, @tenant_id, @lead_id, @holder_party_id, @line_of_business_id, @agent_id, @risk_description, @risk_data, 'open', @at, @at, @actor, 1);

-- name: GetRequest :one
SELECT id, lead_id, holder_party_id, line_of_business_id, agent_id, risk_description, risk_data, status, decline_reason, policy_id, version
FROM quoting.requests WHERE tenant_id = @tenant_id AND id = @id;

-- name: UpdateRequest :execrows
UPDATE quoting.requests SET status = @status, decline_reason = @decline_reason, policy_id = @policy_id, updated_at = @at, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND version = @version;

-- name: ListRequests :many
SELECT id, lead_id, holder_party_id, line_of_business_id, agent_id, risk_description, risk_data, status, decline_reason, policy_id, version
FROM quoting.requests
WHERE tenant_id = @tenant_id
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('agent')::uuid IS NULL OR agent_id = sqlc.narg('agent')::uuid)
  AND (sqlc.narg('before')::uuid IS NULL OR id < sqlc.narg('before')::uuid)
ORDER BY id DESC LIMIT @page_size;

-- name: InsertOffer :exec
INSERT INTO quoting.offers (id, tenant_id, request_id, insurer_id, product_id, reference, currency, net_premium, taxes, effective_from, effective_to, valid_until, status, risk_schema_key, risk_schema_version, created_at, created_by)
VALUES (@id, @tenant_id, @request_id, @insurer_id, @product_id, @reference, @currency, @net_premium, @taxes, @effective_from, @effective_to, @valid_until, 'offered', @risk_schema_key, @risk_schema_version, @at, @actor);

-- name: InsertCoverage :exec
INSERT INTO quoting.offer_coverages (tenant_id, offer_id, position, name, sum_insured, deductible)
VALUES (@tenant_id, @offer_id, @position, @name, @sum_insured, @deductible);

-- name: ListOffers :many
SELECT id, insurer_id, product_id, reference, currency, net_premium, taxes, effective_from, effective_to, valid_until, status, risk_schema_key, risk_schema_version
FROM quoting.offers WHERE tenant_id = @tenant_id AND request_id = @request_id ORDER BY id;

-- name: ListCoverages :many
SELECT c.offer_id, c.name, c.sum_insured, c.deductible
FROM quoting.offer_coverages c JOIN quoting.offers o ON o.tenant_id = c.tenant_id AND o.id = c.offer_id
WHERE c.tenant_id = @tenant_id AND o.request_id = @request_id ORDER BY c.offer_id, c.position;

-- name: SetOfferStatus :exec
UPDATE quoting.offers SET status = @status WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetOfferValidDays :one
SELECT offer_valid_days FROM quoting.settings WHERE tenant_id = @tenant_id;

-- name: SetOfferValidDays :exec
INSERT INTO quoting.settings (tenant_id, offer_valid_days, updated_at, updated_by) VALUES (@tenant_id, @offer_valid_days, @at, @actor)
ON CONFLICT (tenant_id) DO UPDATE SET offer_valid_days = EXCLUDED.offer_valid_days, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by;

-- name: TransferRequests :execrows
-- Traspaso de cartera (DECISIONS N11): mueve las solicitudes abiertas creadas hasta el corte.
UPDATE quoting.requests SET agent_id = @to_agent, updated_at = @at, updated_by = @actor, version = version + 1
WHERE tenant_id = @tenant_id AND agent_id = @from_agent AND status = 'open' AND created_at <= @requested_at;
