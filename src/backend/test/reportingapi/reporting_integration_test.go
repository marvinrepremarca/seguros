//go:build integration

// Package reportingapi prueba los indicadores contra PostgreSQL real, con los datos en las tablas de cada módulo.
package reportingapi_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/reporting"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/pgtest"
)

//nolint:gochecknoglobals // compartidos por la suite (TestMain)
var (
	database *db.DB
	super    *pgx.Conn
)

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	ctx := context.Background()
	env, err := pgtest.Start(ctx)
	if err != nil {
		return 1
	}
	defer env.Terminate() //nolint:errcheck // limpieza al final de la suite
	if err := env.MigrateAll(ctx); err != nil {
		return 1
	}
	if database, err = env.OpenApp(ctx); err != nil {
		return 1
	}
	defer database.Close()
	if super, err = pgx.Connect(ctx, env.SuperuserURL); err != nil {
		return 1
	}
	return m.Run()
}

type principalKey struct{}

// checker apaga por tenant el módulo o la clave indicada.
type checker struct {
	off     map[kernel.TenantID]bool
	offKeys map[flags.Key]bool
}

func (c checker) IsEnabled(ctx context.Context, k flags.Key) bool {
	t, ok := reqctx.TenantID(ctx)
	return ok && !c.off[t] && !c.offKeys[k]
}

type env struct {
	router *chi.Mux
	policy *authz.Policy
	check  checker
}

func newEnv(t *testing.T) *env {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.NewFakeClock(time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC))
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	e := &env{policy: policy, check: checker{off: map[kernel.TenantID]bool{}, offKeys: map[flags.Key]bool{}}}
	mod := reporting.New(reporting.Deps{DB: database, Checker: e.check, Policy: policy, Clock: clock, Logger: logger})
	root := httpx.NewRouter(httpx.RouterOptions{Logger: logger, Clock: clock, MaxBodyBytes: 1 << 20})
	var mountErr error
	root.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := req.Context()
				if p, ok := ctx.Value(principalKey{}).(authz.Principal); ok {
					ctx = reqctx.WithTenantPlan(authz.WithPrincipal(ctx, p), "empresarial")
				}
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		mountErr = mod.Mount(r)
	})
	require.NoError(t, mountErr)
	e.router = root
	return e
}

func (e *env) get(t *testing.T, tenant kernel.TenantID, role authz.Role, query string, out any) int {
	t.Helper()
	who := authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), tenant, []authz.Role{role}, e.policy)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/reporting/kpis"+query, nil)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req.WithContext(context.WithValue(req.Context(), principalKey{}, who)))
	if out != nil && rec.Code < 300 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out), rec.Body.String())
	}
	return rec.Code
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	_, err := super.Exec(context.Background(), sql, args...)
	require.NoError(t, err)
}

func id() uuid.UUID { return uuid.Must(uuid.NewV7()) }

// seed crea, con el superusuario, una póliza expedida con cuota vencida, siniestro pagado, comisión y renovación.
func seed(t *testing.T, tenant uuid.UUID) {
	t.Helper()
	policy, holder, plan := id(), id(), id()
	exec(t, `INSERT INTO policies.policies (id, tenant_id, status, policy_number, insurer_id, line_of_business_id, currency, net_premium, taxes, effective_from, effective_to, payment_form, issued_at, created_at, updated_at, version)
		VALUES ($1, $2, 'active', $3, $4, $5, 'COP', 1000, 190, '2026-06-01', '2027-06-01', 'single_payment', '2026-06-11 12:00+00', '2026-06-10 12:00+00', '2026-06-11 12:00+00', 1)`, policy, tenant, policy.String()[:20], id(), id())
	exec(t, `INSERT INTO policies.policy_parties (tenant_id, policy_id, party_id, role) VALUES ($1, $2, $3, 'holder')`, tenant, policy, holder)
	exec(t, `INSERT INTO billing.plans (id, tenant_id, policy_id, policy_number, insurer_id, holder_party_id, currency, total, credit, cancelled, created_at, updated_at, version)
		VALUES ($1, $2, $3, 'P', $4, $5, 'COP', 1190, 0, false, now(), now(), 1)`, plan, tenant, policy, id(), holder)
	exec(t, `INSERT INTO billing.installments (id, tenant_id, plan_id, number, due_date, amount, paid, status, overdue_notified) VALUES ($1, $2, $3, 1, '2026-05-01', 500, 100, 'pending', false), ($4, $2, $3, 2, '2026-12-01', 690, 0, 'pending', false)`, id(), tenant, plan, id())
	exec(t, `INSERT INTO quoting.requests (id, tenant_id, holder_party_id, line_of_business_id, agent_id, risk_description, risk_data, status, created_at, updated_at, version) VALUES ($1, $2, $3, $4, $5, 'Riesgo', '{}', 'accepted', '2026-06-02 12:00+00', now(), 1), ($6, $2, $3, $4, $5, 'Riesgo', '{}', 'open', '2026-06-02 12:00+00', now(), 1)`, id(), tenant, holder, id(), id(), id())
	exec(t, `INSERT INTO renewals.cases (id, tenant_id, policy_id, policy_number, insurer_id, holder_party_id, effective_to, status, created_at, updated_at, version) VALUES ($1, $2, $3, 'P', $4, $5, '2026-06-20', 'renewed', now(), now(), 1), ($6, $2, $7, 'Q', $4, $5, '2026-06-21', 'lost', now(), now(), 1)`, id(), tenant, id(), id(), holder, id(), id())
	exec(t, `INSERT INTO claims.claims (id, tenant_id, policy_id, insurer_id, holder_party_id, occurred_on, location, claim_type, description, currency, paid_amount, status, alert_out_of_coverage, alert_in_arrears, alert_not_in_force, reported_at, updated_at, version)
		VALUES ($1, $2, $3, $4, $5, '2026-06-05', 'Bogotá', 'Choque', 'Choque simple', 'COP', 250, 'closed', false, false, false, '2026-06-05 12:00+00', '2026-06-08 12:00+00', 1)`, id(), tenant, policy, id(), holder)
	exec(t, `INSERT INTO commissions.accruals (id, tenant_id, policy_id, kind, source_id, base, percentage, amount, currency, accrued_on, missing_agreement, created_at) VALUES ($1, $2, $3, 'payment', $4, 1000, 10, 100, 'COP', '2026-06-12', false, now())`, id(), tenant, policy, id())
	exec(t, `INSERT INTO commissions.policy_terms (tenant_id, policy_id, policy_number, insurer_id, line_id, currency, net_premium, taxes) VALUES ($1, $2, 'P', $3, $4, 'COP', 1000, 190)`, tenant, policy, id(), id())
	statement := id()
	exec(t, `INSERT INTO commissions.statements (id, tenant_id, insurer_id, reference, received_on, created_at) VALUES ($1, $2, $3, 'L-1', '2026-06-13', now())`, statement, tenant, id())
	exec(t, `INSERT INTO commissions.statement_lines (id, tenant_id, statement_id, policy_number, policy_id, reported, expected, matched) VALUES ($1, $2, $3, 'P', $4, 80, 100, false)`, id(), tenant, statement, policy)
}

const period = "?from=2026-06-01&to=2026-06-30"

type kpis struct {
	Premiums []struct {
		Currency, Amount string
	} `json:"premiums"`
	IssuanceDays      string `json:"issuanceDays"`
	PoliciesPerClient struct {
		Policies, Clients int
	} `json:"policiesPerClient"`
	Conversion *struct {
		Requests, Accepted int
	} `json:"conversion"`
	Retention *struct {
		Due, Renewed int
	} `json:"retention"`
	Overdue *[]struct {
		Bucket, Currency, Balance string
	} `json:"overdue"`
	Commissions *[]struct {
		Currency, Accrued, Received string
	} `json:"commissions"`
	ClaimDays *string `json:"claimDays"`
	LossRatio *[]struct {
		Currency, Paid, Premium, Percentage string
	} `json:"lossRatio"`
}

func TestKpisAndTenantIsolation(t *testing.T) {
	e := newEnv(t)
	tenant := kernel.TenantID(id())
	seed(t, uuid.UUID(tenant))
	var k kpis
	require.Equal(t, http.StatusOK, e.get(t, tenant, authz.RoleManager, period, &k))
	require.Len(t, k.Premiums, 1)
	assert.Equal(t, "1000", k.Premiums[0].Amount)
	assert.Equal(t, "1", k.IssuanceDays)
	assert.Equal(t, 1, k.PoliciesPerClient.Policies)
	assert.Equal(t, 1, k.PoliciesPerClient.Clients)
	require.NotNil(t, k.Conversion)
	assert.Equal(t, 2, k.Conversion.Requests)
	assert.Equal(t, 1, k.Conversion.Accepted)
	require.NotNil(t, k.Retention)
	assert.Equal(t, 2, k.Retention.Due)
	assert.Equal(t, 1, k.Retention.Renewed)
	require.NotNil(t, k.Overdue)
	require.Len(t, *k.Overdue, 1, "la cuota de diciembre no está vencida")
	assert.Equal(t, "d31_60", (*k.Overdue)[0].Bucket)
	assert.Equal(t, "400", (*k.Overdue)[0].Balance)
	require.NotNil(t, k.Commissions)
	require.Len(t, *k.Commissions, 1)
	assert.Equal(t, "100", (*k.Commissions)[0].Accrued)
	assert.Equal(t, "80", (*k.Commissions)[0].Received)
	require.NotNil(t, k.ClaimDays)
	assert.Equal(t, "3", *k.ClaimDays)
	require.NotNil(t, k.LossRatio)
	require.Len(t, *k.LossRatio, 1)
	assert.Equal(t, "25", (*k.LossRatio)[0].Percentage)

	var other kpis
	require.Equal(t, http.StatusOK, e.get(t, kernel.TenantID(id()), authz.RoleManager, period, &other))
	assert.Empty(t, other.Premiums)
	assert.Zero(t, other.PoliciesPerClient.Policies)
	assert.Empty(t, *other.Overdue)
}

func TestSectionsFollowModuleFlags(t *testing.T) {
	e := newEnv(t)
	tenant := kernel.TenantID(id())
	seed(t, uuid.UUID(tenant))
	for _, k := range []flags.Key{flags.BillingModule, flags.ClaimsModule, flags.CommissionsModule, flags.QuotingModule, flags.RenewalsModule} {
		e.check.offKeys[k] = true
	}
	var k kpis
	require.Equal(t, http.StatusOK, e.get(t, tenant, authz.RoleManager, period, &k))
	assert.Len(t, k.Premiums, 1, "las pólizas son núcleo")
	assert.Nil(t, k.Conversion)
	assert.Nil(t, k.Retention)
	assert.Nil(t, k.Overdue)
	assert.Nil(t, k.Commissions)
	assert.Nil(t, k.ClaimDays)
	assert.Nil(t, k.LossRatio)
}

func TestPermissionsRangeAndFlag(t *testing.T) {
	e := newEnv(t)
	tenant := kernel.TenantID(id())
	assert.Equal(t, http.StatusForbidden, e.get(t, tenant, authz.RoleSalesAgent, period, nil))
	assert.Equal(t, http.StatusOK, e.get(t, tenant, authz.RoleAccounting, period, nil))
	assert.Equal(t, http.StatusUnprocessableEntity, e.get(t, tenant, authz.RoleManager, "?from=2026-06-30&to=2026-06-01", nil))
	assert.Equal(t, http.StatusBadRequest, e.get(t, tenant, authz.RoleManager, "?from=2026-06-01", nil))
	off := kernel.TenantID(id())
	e.check.off[off] = true
	assert.Equal(t, http.StatusNotFound, e.get(t, off, authz.RoleManager, period, nil))
}
