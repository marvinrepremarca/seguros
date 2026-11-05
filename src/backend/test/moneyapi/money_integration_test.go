//go:build integration

// Package moneyapi prueba comisiones y renovaciones de punta a punta contra PostgreSQL real.
package moneyapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	billingevents "github.com/marvinrepremarca/seguros/internal/modules/billing/contracts"
	catalog "github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/commissions"
	commissionshttp "github.com/marvinrepremarca/seguros/internal/modules/commissions/adapters/http"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals"
	renewalshttp "github.com/marvinrepremarca/seguros/internal/modules/renewals/adapters/http"
	renewalspg "github.com/marvinrepremarca/seguros/internal/modules/renewals/adapters/postgres"
	renewalsapp "github.com/marvinrepremarca/seguros/internal/modules/renewals/app"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/contracttest"
	"github.com/marvinrepremarca/seguros/test/pgtest"
)

//nolint:gochecknoglobals // compartido por la suite (TestMain)
var database *db.DB

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	ctx := context.Background()
	env, err := pgtest.Start(ctx)
	if err != nil {
		slog.Error("iniciando PostgreSQL", "error", err)
		return 1
	}
	defer env.Terminate() //nolint:errcheck // limpieza al final de la suite
	if err := env.MigrateAll(ctx); err != nil {
		slog.Error("migrando", "error", err)
		return 1
	}
	if database, err = env.OpenApp(ctx); err != nil {
		return 1
	}
	defer database.Close()
	return m.Run()
}

type principalKey struct{}

type checker struct{ off map[kernel.TenantID]bool }

func (c checker) IsEnabled(ctx context.Context, _ flags.Key) bool {
	t, ok := reqctx.TenantID(ctx)
	return ok && !c.off[t]
}

type directory struct{}

func (directory) Summaries(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]parties.PartySummary, error) {
	out := map[uuid.UUID]parties.PartySummary{}
	for _, id := range ids {
		out[id] = parties.PartySummary{Name: "Ana"}
	}
	return out, nil
}

// agreements simula el acuerdo pactado en catalog.
type agreements struct {
	basis string
	pct   string
}

func (a *agreements) ResolvePolicyRef(context.Context, catalog.PolicyRef) (string, error) {
	return "auto", nil
}

func (a *agreements) AgreementFor(context.Context, catalog.PolicyRef, kernel.Date) (catalog.Agreement, bool, error) {
	if a.basis == "" {
		return catalog.Agreement{}, false, nil
	}
	pct, err := kernel.NewPercentage(decimal.RequireFromString(a.pct))
	return catalog.Agreement{Basis: a.basis, Percentage: pct}, true, err
}

type env struct {
	router    *chi.Mux
	policy    *authz.Policy
	clock     *kernel.FakeClock
	off       map[kernel.TenantID]bool
	agreement *agreements
	consumers []outbox.Consumer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.NewFakeClock(time.Now().UTC()) //nolint:forbidigo // partición de auditoría del mes actual
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	e := &env{policy: policy, clock: clock, off: map[kernel.TenantID]bool{}, agreement: &agreements{basis: "paid_premium", pct: "10"}}
	recorder, writer := audit.NewRecorder(clock), outbox.NewWriter(clock)
	comm := commissions.New(commissions.Deps{DB: database, Audit: recorder, Checker: checker{off: e.off}, Policy: policy, Clock: clock, Logger: logger})
	ren := renewals.New(renewals.Deps{DB: database, Audit: recorder, Outbox: writer, Parties: directory{}, Checker: checker{off: e.off}, Policy: policy, Clock: clock, Logger: logger})
	e.consumers = append(commissions.Consumers(database, recorder, e.agreement, clock), renewals.Consumers(database, recorder, writer, clock)...)
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
		mountErr = errors.Join(comm.Mount(r), ren.Mount(r))
	})
	require.NoError(t, mountErr)
	e.router = root
	return e
}

func (e *env) as(tenant kernel.TenantID, roles ...authz.Role) authz.Principal {
	return authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), tenant, roles, e.policy)
}

func (e *env) do(t *testing.T, who authz.Principal, method, path string, body, out any) int {
	t.Helper()
	ctx := context.WithValue(context.Background(), principalKey{}, who)
	var rd bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&rd).Encode(body))
	}
	req := httptest.NewRequestWithContext(ctx, method, path, &rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	contracttest.New(t, commissionshttp.GetSpec, renewalshttp.GetSpec).Check(t, req, rec)
	if out != nil && rec.Code < 300 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out), rec.Body.String())
	}
	return rec.Code
}

// deliver entrega el evento a los consumidores del módulo indicado.
func (e *env) deliver(t *testing.T, tenant kernel.TenantID, flag flags.Key, topic string, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	ctx := reqctx.WithTenantID(context.Background(), tenant)
	found := false
	for _, c := range e.consumers {
		if c.Topic != topic || c.Flag != flag {
			continue
		}
		found = true
		ev := outbox.Event{ID: uuid.Must(uuid.NewV7()), TenantID: tenant, Topic: topic, Payload: raw}
		require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error { return c.Handle(ctx, tx, ev) }))
	}
	require.True(t, found, "sin consumidor para %s", topic)
}

func newTenant() kernel.TenantID { return kernel.TenantID(uuid.Must(uuid.NewV7())) }

type issued struct {
	id, insurer, agent uuid.UUID
	number             string
}

func (e *env) issue(t *testing.T, tenant kernel.TenantID, flag flags.Key, to string, renewedFrom *uuid.UUID) issued {
	t.Helper()
	p := issued{id: uuid.Must(uuid.NewV7()), insurer: uuid.Must(uuid.NewV7()), agent: uuid.Must(uuid.NewV7())}
	p.number = "P-" + p.id.String()[:8]
	e.deliver(t, tenant, flag, policies.TopicPolicyIssued, policies.PolicyIssued{
		PolicyID: p.id, PolicyNumber: p.number, InsurerID: p.insurer, LineOfBusinessID: uuid.Must(uuid.NewV7()), HolderPartyID: uuid.Must(uuid.NewV7()), AgentID: &p.agent,
		Currency: "COP", NetPremium: "1000000", Taxes: "190000", EffectiveFrom: "2026-01-01", EffectiveTo: to, PaymentForm: "single_payment", RenewedFromPolicyID: renewedFrom,
	})
	return p
}

type accrual struct {
	Kind       string `json:"kind"`
	Amount     string `json:"amount"`
	Base       string `json:"base"`
	AgentID    string `json:"agentId"`
	Reconciled bool   `json:"reconciled"`
	Missing    bool   `json:"missingAgreement"`
}

func (e *env) accruals(t *testing.T, who authz.Principal) []accrual {
	t.Helper()
	var page struct {
		Items []accrual `json:"items"`
	}
	require.Equal(t, http.StatusOK, e.do(t, who, http.MethodGet, "/api/v1/commissions/accruals", nil, &page))
	return page.Items
}

func TestPaidPremiumAccrualUsesNetShareAndReversal(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	acc := e.as(tenant, authz.RoleAccounting)
	p := e.issue(t, tenant, flags.CommissionsModule, "2026-12-31", nil)
	assert.Empty(t, e.accruals(t, acc), "con prima pagada no se causa al expedir")

	pay := uuid.Must(uuid.NewV7())
	e.deliver(t, tenant, flags.CommissionsModule, billingevents.TopicPaymentApplied, billingevents.PaymentApplied{PolicyID: p.id, PaymentID: pay, Currency: "COP", Amount: "119000", PaidOn: "2026-02-01"})
	items := e.accruals(t, acc)
	require.Len(t, items, 1)
	assert.Equal(t, "100000", items[0].Base, "parte neta del pago (N14)")
	assert.Equal(t, "10000", items[0].Amount)
	assert.Equal(t, p.agent.String(), items[0].AgentID)

	e.deliver(t, tenant, flags.CommissionsModule, billingevents.TopicPaymentReversed, billingevents.PaymentReversed{PolicyID: p.id, PaymentID: pay, Currency: "COP", Amount: "119000"})
	items = e.accruals(t, acc)
	require.Len(t, items, 2)
	assert.Equal(t, "-10000", items[1].Amount)
}

func TestWrittenBasisEndorsementClawbackAndMissingAgreement(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	acc := e.as(tenant, authz.RoleAccounting)
	e.agreement.basis = "written_premium"
	p := e.issue(t, tenant, flags.CommissionsModule, "2026-12-31", nil)
	e.deliver(t, tenant, flags.CommissionsModule, policies.TopicPolicyEndorsed, policies.PolicyEndorsed{
		PolicyID: p.id, EndorsementID: uuid.Must(uuid.NewV7()), Currency: "COP", NetPremiumDelta: "-100000", TaxesDelta: "-19000", EffectiveDate: "2026-03-01",
	})
	e.deliver(t, tenant, flags.CommissionsModule, policies.TopicPolicyCancelled, policies.PolicyCancelled{
		PolicyID: p.id, Cause: "client_request", EffectiveDate: "2026-06-01", Currency: "COP", UnearnedPremium: "500000",
	})
	items := e.accruals(t, acc)
	require.Len(t, items, 3)
	assert.Equal(t, "100000", items[0].Amount)
	assert.Equal(t, "-10000", items[1].Amount)
	assert.Equal(t, "clawback", items[2].Kind)
	assert.Equal(t, "-50000", items[2].Amount)

	e.agreement.basis = ""
	q := e.issue(t, tenant, flags.CommissionsModule, "2026-12-31", nil)
	e.deliver(t, tenant, flags.CommissionsModule, billingevents.TopicPaymentApplied, billingevents.PaymentApplied{PolicyID: q.id, PaymentID: uuid.Must(uuid.NewV7()), Currency: "COP", Amount: "119000", PaidOn: "2026-02-01"})
	items = e.accruals(t, acc)
	assert.True(t, items[len(items)-1].Missing, "sin acuerdo queda en 0 % con alerta")
}

func TestReconciliationAndAgentCommissionFollowTransfer(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	acc, mgr := e.as(tenant, authz.RoleAccounting), e.as(tenant, authz.RoleManager)
	p := e.issue(t, tenant, flags.CommissionsModule, "2026-12-31", nil)
	before, after := p.agent, uuid.Must(uuid.NewV7())
	for _, a := range []uuid.UUID{before, after} {
		require.Equal(t, http.StatusCreated, e.do(t, mgr, http.MethodPost, "/api/v1/commissions/agent-rates", map[string]any{"agentId": a, "percentage": "50", "validFrom": "2026-01-01"}, nil))
	}
	e.deliver(t, tenant, flags.CommissionsModule, policies.TopicPolicyAgentChanged, policies.PolicyAgentChanged{
		PolicyID: p.id, PreviousAgentID: &before, AgentID: &after, EffectiveAt: "2026-03-01T17:00:00Z",
	})
	pay := func(on string) {
		e.deliver(t, tenant, flags.CommissionsModule, billingevents.TopicPaymentApplied, billingevents.PaymentApplied{PolicyID: p.id, PaymentID: uuid.Must(uuid.NewV7()), Currency: "COP", Amount: "119000", PaidOn: on})
	}
	pay("2026-02-01")
	pay("2026-04-01")

	statement := func(reference, amount string) (int, map[string]any) {
		out := map[string]any{}
		code := e.do(t, acc, http.MethodPost, "/api/v1/commissions/statements", map[string]any{
			"insurerId": p.insurer, "reference": reference, "receivedOn": "2026-05-01",
			"lines": []map[string]any{{"policyNumber": p.number, "amount": amount}},
		}, &out)
		return code, out
	}
	code, res := statement("L-1", "15000")
	require.Equal(t, http.StatusCreated, code)
	assert.EqualValues(t, 1, res["discrepancies"])
	var disc struct {
		Items []struct {
			Expected string `json:"expected"`
		} `json:"items"`
	}
	require.Equal(t, http.StatusOK, e.do(t, acc, http.MethodGet, "/api/v1/commissions/discrepancies", nil, &disc))
	require.Len(t, disc.Items, 1)
	assert.Equal(t, "20000", disc.Items[0].Expected)

	code, _ = statement("L-1", "20000")
	assert.Equal(t, http.StatusConflict, code, "misma liquidación")

	code, res = statement("L-2", "20500")
	require.Equal(t, http.StatusCreated, code)
	assert.EqualValues(t, 1, res["matched"], "dentro de la tolerancia")
	require.Equal(t, http.StatusOK, e.do(t, acc, http.MethodGet, "/api/v1/commissions/discrepancies", nil, &disc))
	assert.Empty(t, disc.Items, "la nueva línea cierra la diferencia")

	var agents struct {
		Items []struct {
			AgentID string `json:"agentId"`
			Amount  string `json:"amount"`
		} `json:"items"`
	}
	require.Equal(t, http.StatusOK, e.do(t, acc, http.MethodGet, "/api/v1/commissions/agent-commissions", nil, &agents))
	require.Len(t, agents.Items, 2)
	got := map[string]string{}
	for _, a := range agents.Items {
		got[a.AgentID] = a.Amount
	}
	assert.Equal(t, map[string]string{before.String(): "5000", after.String(): "5000"}, got, "cada uno lo causado en su periodo (N16)")
}

func TestUnknownPolicyAndNoRateAreFlagged(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	acc := e.as(tenant, authz.RoleAccounting)
	p := e.issue(t, tenant, flags.CommissionsModule, "2026-12-31", nil)
	e.deliver(t, tenant, flags.CommissionsModule, billingevents.TopicPaymentApplied, billingevents.PaymentApplied{PolicyID: p.id, PaymentID: uuid.Must(uuid.NewV7()), Currency: "COP", Amount: "119000", PaidOn: "2026-02-01"})
	res := map[string]any{}
	require.Equal(t, http.StatusCreated, e.do(t, acc, http.MethodPost, "/api/v1/commissions/statements", map[string]any{
		"insurerId": p.insurer, "reference": "L-1", "receivedOn": "2026-05-01",
		"lines": []map[string]any{{"policyNumber": p.number, "amount": "10000"}, {"policyNumber": "NO-EXISTE", "amount": "1"}},
	}, &res))
	assert.EqualValues(t, 1, res["matched"])
	assert.EqualValues(t, 1, res["discrepancies"])
	var agents struct {
		Items []struct {
			MissingRate bool   `json:"missingRate"`
			Amount      string `json:"amount"`
		} `json:"items"`
	}
	require.Equal(t, http.StatusOK, e.do(t, acc, http.MethodGet, "/api/v1/commissions/agent-commissions", nil, &agents))
	require.Len(t, agents.Items, 1)
	assert.True(t, agents.Items[0].MissingRate, "sin tasa: 0 % y alerta (N5)")
	assert.Equal(t, "0", agents.Items[0].Amount)
}

func TestCommissionsFlagIsolationAndPermissions(t *testing.T) {
	e := newEnv(t)
	a, b, off := newTenant(), newTenant(), newTenant()
	e.off[off] = true
	e.issue(t, a, flags.CommissionsModule, "2026-12-31", nil)
	const path = "/api/v1/commissions/accruals"
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(off, authz.RoleAccounting), http.MethodGet, path, nil, nil))
	assert.Equal(t, http.StatusForbidden, e.do(t, e.as(a, authz.RoleSalesAgent), http.MethodGet, path, nil, nil))
	assert.Equal(t, http.StatusForbidden, e.do(t, e.as(a, authz.RoleAccounting), http.MethodGet, "/api/v1/commissions/settings", nil, nil))
	assert.Empty(t, e.accruals(t, e.as(b, authz.RoleAccounting)))
	ctx := reqctx.WithTenantID(context.Background(), b)
	var n int
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM commissions.policy_terms`).Scan(&n)
	}))
	assert.Zero(t, n, "tenant B no ve la proyección de A")
}

type renewalCase struct {
	PolicyID string `json:"policyId"`
	Status   string `json:"status"`
}

func (e *env) cases(t *testing.T, who authz.Principal, query string) []renewalCase {
	t.Helper()
	var page struct {
		Items []renewalCase `json:"items"`
	}
	require.Equal(t, http.StatusOK, e.do(t, who, http.MethodGet, "/api/v1/renewals/cases"+query, nil, &page))
	return page.Items
}

func TestRenewalCaseLifecycleAndRetention(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	ops := e.as(tenant, authz.RoleOperations)
	first := e.issue(t, tenant, flags.RenewalsModule, "2026-12-31", nil)
	lost := e.issue(t, tenant, flags.RenewalsModule, "2026-12-31", nil)
	cancelled := e.issue(t, tenant, flags.RenewalsModule, "2026-12-31", nil)
	require.Len(t, e.cases(t, ops, "?status=open"), 3)

	base := "/api/v1/renewals/cases/"
	require.Equal(t, http.StatusOK, e.do(t, ops, http.MethodPost, base+first.id.String()+"/start", nil, nil))
	assert.Equal(t, http.StatusConflict, e.do(t, ops, http.MethodPost, base+first.id.String()+"/start", nil, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, ops, http.MethodPost, base+uuid.NewString()+"/start", nil, nil))
	require.Equal(t, http.StatusOK, e.do(t, ops, http.MethodPost, base+lost.id.String()+"/lost", map[string]any{"reason": "Precio alto"}, nil))
	assert.Equal(t, http.StatusForbidden, e.do(t, e.as(tenant, authz.RoleManager), http.MethodPost, base+lost.id.String()+"/start", nil, nil))

	e.issue(t, tenant, flags.RenewalsModule, "2027-12-31", &first.id)
	e.deliver(t, tenant, flags.RenewalsModule, policies.TopicPolicyCancelled, policies.PolicyCancelled{PolicyID: cancelled.id, EffectiveDate: "2026-06-01", Currency: "COP", UnearnedPremium: "0"})
	assert.Len(t, e.cases(t, ops, "?status=renewed"), 1)
	assert.Len(t, e.cases(t, ops, "?status=closed"), 1)

	var ret struct {
		Due     int `json:"due"`
		Renewed int `json:"renewed"`
	}
	require.Equal(t, http.StatusOK, e.do(t, ops, http.MethodGet, "/api/v1/renewals/retention?from=2026-12-01&to=2026-12-31", nil, &ret))
	assert.Equal(t, 2, ret.Due, "la cancelada no cuenta")
	assert.Equal(t, 1, ret.Renewed)
}

func TestRenewalAlertsAreSentOnceAndRespectSettings(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	admin := e.as(tenant, authz.RoleTenantAdmin)
	to := kernel.Today(e.clock).AddDays(20)
	p := e.issue(t, tenant, flags.RenewalsModule, to.String(), nil)
	store := renewalspg.NewStore(database, audit.NewRecorder(e.clock), outbox.NewWriter(e.clock))
	ctx := reqctx.WithTenantID(context.Background(), tenant)
	sent := func() int {
		var n int
		require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM renewals.alerts WHERE policy_id = $1`, p.id).Scan(&n)
		}))
		return n
	}
	require.NoError(t, renewalsapp.SendAlerts(ctx, store, e.clock))
	assert.Equal(t, 2, sent(), "cruzó 60 y 30 (T2)")
	require.NoError(t, renewalsapp.SendAlerts(ctx, store, e.clock))
	assert.Equal(t, 2, sent(), "no repite")

	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, admin, http.MethodPut, "/api/v1/renewals/settings", map[string]any{"alertDays": []int{30, 30}}, nil))
	require.Equal(t, http.StatusNoContent, e.do(t, admin, http.MethodPut, "/api/v1/renewals/settings", map[string]any{"alertDays": []int{25}}, nil))
	require.NoError(t, renewalsapp.SendAlerts(ctx, store, e.clock))
	assert.Equal(t, 3, sent(), "el nuevo umbral también")
}

func TestRenewalsFlagOff(t *testing.T) {
	e := newEnv(t)
	off := newTenant()
	e.off[off] = true
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(off, authz.RoleOperations), http.MethodGet, "/api/v1/renewals/cases", nil, nil))
}

func TestReversalDeliveredBeforeItsPaymentCancelsTheAccrual(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	acc := e.as(tenant, authz.RoleAccounting)
	p := e.issue(t, tenant, flags.CommissionsModule, "2026-12-31", nil)
	pay := uuid.Must(uuid.NewV7())
	e.deliver(t, tenant, flags.CommissionsModule, billingevents.TopicPaymentReversed, billingevents.PaymentReversed{PolicyID: p.id, PaymentID: pay, Currency: "COP", Amount: "119000"})
	e.deliver(t, tenant, flags.CommissionsModule, billingevents.TopicPaymentApplied, billingevents.PaymentApplied{PolicyID: p.id, PaymentID: pay, Currency: "COP", Amount: "119000", PaidOn: "2026-02-01"})
	items := e.accruals(t, acc)
	require.Len(t, items, 1, "el pago reversado no causa")
	assert.Equal(t, "0", items[0].Amount)
}

func TestToleranceOnlyAppliesToCOP(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	acc := e.as(tenant, authz.RoleAccounting)
	p := issued{id: uuid.Must(uuid.NewV7()), insurer: uuid.Must(uuid.NewV7()), number: "USD-1"}
	e.deliver(t, tenant, flags.CommissionsModule, policies.TopicPolicyIssued, policies.PolicyIssued{
		PolicyID: p.id, PolicyNumber: p.number, InsurerID: p.insurer, LineOfBusinessID: uuid.Must(uuid.NewV7()), HolderPartyID: uuid.Must(uuid.NewV7()),
		Currency: "USD", NetPremium: "1000", Taxes: "0", EffectiveFrom: "2026-01-01", EffectiveTo: "2026-12-31", PaymentForm: "single_payment",
	})
	e.deliver(t, tenant, flags.CommissionsModule, billingevents.TopicPaymentApplied, billingevents.PaymentApplied{PolicyID: p.id, PaymentID: uuid.Must(uuid.NewV7()), Currency: "USD", Amount: "1000", PaidOn: "2026-02-01"})
	res := map[string]any{}
	require.Equal(t, http.StatusCreated, e.do(t, acc, http.MethodPost, "/api/v1/commissions/statements", map[string]any{
		"insurerId": p.insurer, "reference": "L-1", "receivedOn": "2026-05-01", "lines": []map[string]any{{"policyNumber": p.number, "amount": "99"}},
	}, &res))
	assert.EqualValues(t, 1, res["discrepancies"], "USD 1 de diferencia no se tolera")

	// La agencia puede fijar tolerancia para otra moneda.
	mgr := e.as(tenant, authz.RoleManager)
	require.Equal(t, http.StatusNoContent, e.do(t, mgr, http.MethodPut, "/api/v1/commissions/settings", map[string]any{"currency": "USD", "tolerance": "5"}, nil))
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, mgr, http.MethodPut, "/api/v1/commissions/settings", map[string]any{"currency": "COP", "tolerance": "400001"}, nil))
	require.Equal(t, http.StatusCreated, e.do(t, acc, http.MethodPost, "/api/v1/commissions/statements", map[string]any{
		"insurerId": p.insurer, "reference": "L-2", "receivedOn": "2026-05-01", "lines": []map[string]any{{"policyNumber": p.number, "amount": "99"}},
	}, &res))
	assert.EqualValues(t, 1, res["matched"])
}
