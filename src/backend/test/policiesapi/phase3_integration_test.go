//go:build integration

package policiesapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/app"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

func (e *env) store() *postgres.Store {
	return postgres.NewStore(workerDB, audit.NewRecorder(e.clock), outbox.NewWriter(e.clock))
}

func (e *env) status(t *testing.T, w world, id string) any {
	t.Helper()
	return e.do(t, w.ops, http.MethodGet, base+"/"+id, nil).body["status"]
}

func TestArrearsFollowBillingEvents(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	id := e.issued(t, w).str(t, "id")
	policyID := uuid.MustParse(id)
	ctx := reqctx.WithTenantID(context.Background(), w.tenant)
	run := func(fn func(context.Context, app.UnitOfWork, kernel.Clock, uuid.UUID) error) {
		require.NoError(t, e.store().WithinTenant(ctx, func(ctx context.Context, uow app.UnitOfWork) error {
			return fn(ctx, uow, e.clock, policyID)
		}))
	}

	run(app.ReinstateAfterPayment)
	assert.Equal(t, "active", e.status(t, w, id), "al día: no aplica")

	run(app.MarkInArrears)
	assert.Equal(t, "in_arrears", e.status(t, w, id))
	run(app.MarkInArrears)
	assert.Equal(t, "in_arrears", e.status(t, w, id), "otra cuota vencida no falla")
	assert.Contains(t, outboxTopics(t, w.tenant), "policies.policy_fell_into_arrears")

	run(app.ReinstateAfterPayment)
	assert.Equal(t, "active", e.status(t, w, id))
	assert.Contains(t, outboxTopics(t, w.tenant), "policies.policy_reinstated")
}

func (w world) renewalTerms() map[string]any {
	terms := w.terms()
	terms["effectiveFrom"], terms["effectiveTo"] = "2027-01-01", "2027-12-31"
	return terms
}

func (e *env) renew(t *testing.T, w world, id string) resp {
	t.Helper()
	r := e.do(t, w.ops, http.MethodPost, base+"/"+id+"/renew", w.renewalTerms())
	require.Equal(t, http.StatusCreated, r.status, r.body)
	return r
}

func (e *env) issue(t *testing.T, w world, r resp) {
	t.Helper()
	id := r.str(t, "id")
	a := e.do(t, w.ops, http.MethodPost, at(id, r.version(t), "accept"), nil)
	require.Equal(t, http.StatusOK, a.status, a.body)
	i := e.do(t, w.ops, http.MethodPost, at(id, a.version(t), "issue"), map[string]any{"policyNumber": "REN-" + uuid.NewString()[:8]})
	require.Equal(t, http.StatusOK, i.status, i.body)
}

func TestEarlyRenewal(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	id := e.issued(t, w).str(t, "id")

	overlap := w.terms()
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, w.ops, http.MethodPost, base+"/"+id+"/renew", overlap).status, "la vigencia no se solapa")

	next := e.renew(t, w, id)
	assert.Equal(t, http.StatusConflict, e.do(t, w.ops, http.MethodPost, base+"/"+id+"/renew", w.renewalTerms()).status, "una sola renovación en curso")
	assert.Equal(t, "active", e.status(t, w, id), "la anterior no cambia")
	e.issue(t, w, next)
	assert.Equal(t, "active", e.status(t, w, id), "sigue vigente hasta su fin (N17)")

	expirer := &app.Expirer{Store: e.store(), Clock: kernel.NewFakeClock(time.Date(2027, 1, 1, 12, 0, 0, 0, time.UTC))}
	_, err := expirer.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "renewed", e.status(t, w, id), "al vencer pasa a Renovada")
}

func TestRenewalAfterExpiryAndNotRenewed(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	renewedLate := e.issued(t, w).str(t, "id")
	lost := e.issued(t, w).str(t, "id")
	ctx := reqctx.WithTenantID(context.Background(), w.tenant)
	expirer := &app.Expirer{Store: e.store(), Clock: kernel.NewFakeClock(time.Date(2027, 1, 1, 12, 0, 0, 0, time.UTC))}
	_, err := expirer.Run(context.Background())
	require.NoError(t, err)
	require.Equal(t, "expired", e.status(t, w, renewedLate))

	next := e.renew(t, w, renewedLate)
	e.issue(t, w, next)
	require.NoError(t, e.store().WithinTenant(ctx, func(ctx context.Context, uow app.UnitOfWork) error {
		return app.RenewPredecessor(ctx, uow, e.clock, uuid.MustParse(renewedLate), uuid.MustParse(next.str(t, "id")))
	}))
	assert.Equal(t, "renewed", e.status(t, w, renewedLate))

	require.NoError(t, e.store().WithinTenant(ctx, func(ctx context.Context, uow app.UnitOfWork) error {
		return app.MarkNotRenewed(ctx, uow, e.clock, uuid.MustParse(lost))
	}))
	assert.Equal(t, "not_renewed", e.status(t, w, lost))
	assert.Equal(t, http.StatusConflict, e.do(t, w.ops, http.MethodPost, base+"/"+lost+"/renew", w.renewalTerms()).status, "terminada no se renueva")
}

func TestManualReinstateAndRenewalKeepsHolder(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	r := e.issued(t, w)
	id := r.str(t, "id")
	assert.Equal(t, http.StatusConflict, e.do(t, w.ops, http.MethodPost, at(id, r.version(t), "reinstate"), nil).status, "al día no aplica")
	ctx := reqctx.WithTenantID(context.Background(), w.tenant)
	require.NoError(t, e.store().WithinTenant(ctx, func(ctx context.Context, uow app.UnitOfWork) error {
		return app.MarkInArrears(ctx, uow, e.clock, uuid.MustParse(id))
	}))
	cur := e.do(t, w.ops, http.MethodGet, base+"/"+id, nil)
	ok := e.do(t, w.ops, http.MethodPost, at(id, cur.version(t), "reinstate"), nil)
	require.Equal(t, http.StatusOK, ok.status, ok.body)
	assert.Equal(t, "active", ok.body["status"])

	changed := w.renewalTerms()
	changed["parties"] = []any{
		map[string]any{"partyId": w.insured, "role": "holder"},
		map[string]any{"partyId": w.holder, "role": "insured"},
	}
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, w.ops, http.MethodPost, base+"/"+id+"/renew", changed).status)

	// La agencia puede relajar la regla (N25).
	const rules = "/api/v1/policies/settings/renewal"
	admin := e.as(w.tenant, authz.RoleTenantAdmin)
	body := map[string]any{"keepHolder": false, "keepLine": true}
	assert.Equal(t, http.StatusForbidden, e.do(t, w.ops, http.MethodPut, rules, body).status)
	require.Equal(t, http.StatusNoContent, e.do(t, admin, http.MethodPut, rules, body).status)
	assert.Equal(t, http.StatusCreated, e.do(t, w.ops, http.MethodPost, base+"/"+id+"/renew", changed).status)
}
