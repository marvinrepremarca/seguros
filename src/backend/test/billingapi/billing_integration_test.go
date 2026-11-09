//go:build integration

// Package billingapi prueba cartera y cobro de punta a punta contra PostgreSQL real.
package billingapi_test

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/billing"
	billingevents "github.com/marvinrepremarca/seguros/internal/modules/billing/adapters/events"
	billinghttp "github.com/marvinrepremarca/seguros/internal/modules/billing/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/billing/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/billing/app"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
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
var (
	database *db.DB
	superURL string
)

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
	superURL = env.SuperuserURL
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

type env struct {
	router *chi.Mux
	policy *authz.Policy
	clock  *kernel.FakeClock
	off    map[kernel.TenantID]bool
	store  *postgres.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.NewFakeClock(time.Now().UTC()) //nolint:forbidigo // partición de auditoría del mes actual
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	e := &env{policy: policy, clock: clock, off: map[kernel.TenantID]bool{}}
	recorder, writer := audit.NewRecorder(clock), outbox.NewWriter(clock)
	e.store = postgres.NewStore(database, recorder, writer, clock)
	mod := billing.New(billing.Deps{DB: database, Audit: recorder, Outbox: writer, Parties: directory{}, Checker: checker{off: e.off}, Policy: policy, Clock: clock, Logger: logger})
	root := httpx.NewRouter(httpx.RouterOptions{Logger: logger, Clock: clock, MaxBodyBytes: 1 << 20})
	var mountErr error
	root.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := req.Context()
				if p, ok := ctx.Value(principalKey{}).(authz.Principal); ok {
					ctx = reqctx.WithTenantPlan(authz.WithPrincipal(ctx, p), "profesional")
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

func (e *env) as(tenant kernel.TenantID, roles ...authz.Role) authz.Principal {
	return authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), tenant, roles, e.policy)
}

type resp struct {
	status int
	raw    []byte
}

func (r resp) into(t *testing.T, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(r.raw, v), string(r.raw))
}

func (e *env) do(t *testing.T, who authz.Principal, method, path string, body any, headers ...string) resp {
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
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	contracttest.New(t, billinghttp.GetSpec).Check(t, req, rec)
	return resp{status: rec.Code, raw: rec.Body.Bytes()}
}

func tenantCtx(tenant kernel.TenantID) context.Context {
	return reqctx.WithTenantID(context.Background(), tenant)
}

// deliver entrega un evento al consumidor real.
func (e *env) deliver(t *testing.T, tenant kernel.TenantID, topic string, payload any) error {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	ctx := tenantCtx(tenant)
	for _, c := range billingevents.Consumers(e.store, e.clock) {
		if c.Topic != topic {
			continue
		}
		assert.Equal(t, flags.BillingModule, c.Flag, "el consumidor respeta el flag del módulo")
		ev := outbox.Event{ID: uuid.Must(uuid.NewV7()), TenantID: tenant, Topic: topic, Payload: raw}
		return database.WithTenantTx(ctx, func(tx pgx.Tx) error { return c.Handle(ctx, tx, ev) })
	}
	return errors.New("sin consumidor")
}

func (e *env) issue(t *testing.T, tenant kernel.TenantID, form, total, from, to string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	require.NoError(t, e.deliver(t, tenant, policies.TopicPolicyIssued, policies.PolicyIssued{
		PolicyID: id, PolicyNumber: "P-" + id.String()[:8], InsurerID: uuid.Must(uuid.NewV7()), HolderPartyID: uuid.Must(uuid.NewV7()),
		Currency: "COP", NetPremium: total, Taxes: "0", EffectiveFrom: from, EffectiveTo: to, PaymentForm: form,
	}))
	return id
}

func newTenant() kernel.TenantID { return kernel.TenantID(uuid.Must(uuid.NewV7())) }

type plan struct {
	Credit       string `json:"credit"`
	Total        string `json:"total"`
	InArrears    bool   `json:"inArrears"`
	Version      int    `json:"version"`
	Cancelled    bool   `json:"cancelled"`
	RefundAmount string `json:"refundAmount"`
	Installments []struct {
		Amount string `json:"amount"`
		Status string `json:"status"`
	} `json:"installments"`
}

func paymentBody(amount string) map[string]any {
	return map[string]any{"amount": amount, "paidOn": "2026-02-01", "method": "transfer", "reportedBy": "client", "supportReference": "REF-1"}
}

func notified(t *testing.T, tenant kernel.TenantID) int {
	t.Helper()
	var n int
	ctx := tenantCtx(tenant)
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM billing.installments WHERE overdue_notified`).Scan(&n)
	}))
	return n
}

func TestPaymentLifecycle(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	collector, reader := e.as(tenant, authz.RoleCollections), e.as(tenant, authz.RoleAccounting)
	policy := e.issue(t, tenant, "monthly_installments", "300", "2026-01-01", "2026-03-31")
	base := "/api/v1/billing/plans/" + policy.String()

	var p plan
	r := e.do(t, reader, http.MethodGet, base, nil)
	require.Equal(t, http.StatusOK, r.status)
	r.into(t, &p)
	require.Len(t, p.Installments, 3)

	r = e.do(t, reader, http.MethodPost, base+"/payments", paymentBody("150"), "Idempotency-Key", "k1")
	assert.Equal(t, http.StatusForbidden, r.status, "Contabilidad no registra pagos")
	r = e.do(t, collector, http.MethodPost, base+"/payments", paymentBody("350"), "Idempotency-Key", "k1")
	require.Equal(t, http.StatusCreated, r.status, string(r.raw))
	var pay struct {
		ID      string `json:"id"`
		Surplus string `json:"surplus"`
	}
	r.into(t, &pay)
	assert.Equal(t, "50", pay.Surplus)

	var again struct {
		ID string `json:"id"`
	}
	r = e.do(t, collector, http.MethodPost, base+"/payments", paymentBody("350"), "Idempotency-Key", "k1")
	require.Equal(t, http.StatusCreated, r.status)
	r.into(t, &again)
	assert.Equal(t, pay.ID, again.ID, "misma llave, mismo pago")
	r = e.do(t, collector, http.MethodPost, base+"/payments", paymentBody("1"), "Idempotency-Key", "k1")
	assert.Equal(t, http.StatusConflict, r.status, "misma llave, otros datos")

	e.do(t, reader, http.MethodGet, base, nil).into(t, &p)
	assert.Equal(t, "50", p.Credit, "se aplicó una sola vez")

	rev := base + "/payments/" + pay.ID + "/reverse"
	r = e.do(t, collector, http.MethodPost, rev, map[string]any{"reason": "error de digitación"})
	require.Equal(t, http.StatusNoContent, r.status, string(r.raw))
	r = e.do(t, collector, http.MethodPost, rev, map[string]any{"reason": "error de digitación"})
	assert.Equal(t, http.StatusConflict, r.status, "un solo reverso")
	var after plan
	e.do(t, reader, http.MethodGet, base, nil).into(t, &after)
	assert.Equal(t, "0", after.Credit)
	for _, i := range after.Installments {
		assert.Equal(t, "pending", i.Status)
	}

	r = e.do(t, collector, http.MethodPost, base+"/collection-activities", map[string]any{"channel": "call", "note": "Promete pagar"})
	assert.Equal(t, http.StatusCreated, r.status, string(r.raw))
}

func TestPaymentsAreImmutableForTheAppRole(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	policy := e.issue(t, tenant, "single_payment", "100", "2026-01-01", "2026-12-31")
	r := e.do(t, e.as(tenant, authz.RoleCollections), http.MethodPost, "/api/v1/billing/plans/"+policy.String()+"/payments", paymentBody("100"), "Idempotency-Key", "k")
	require.Equal(t, http.StatusCreated, r.status)
	ctx := tenantCtx(tenant)
	for _, q := range []string{`UPDATE billing.payments SET amount = 1`, `DELETE FROM billing.payments`} {
		err := database.WithTenantTx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, q)
			return err
		})
		require.Error(t, err, q)
	}
}

func TestFlagOffAndTenantIsolation(t *testing.T) {
	e := newEnv(t)
	a, b, off := newTenant(), newTenant(), newTenant()
	e.off[off] = true
	policy := e.issue(t, a, "single_payment", "100", "2026-01-01", "2026-12-31")
	path := "/api/v1/billing/plans/" + policy.String()

	assert.Equal(t, http.StatusOK, e.do(t, e.as(a, authz.RoleCollections), http.MethodGet, path, nil).status)
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(b, authz.RoleCollections), http.MethodGet, path, nil).status, "otro tenant")
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(off, authz.RoleCollections), http.MethodGet, path, nil).status, "flag apagado")
	assert.Equal(t, http.StatusForbidden, e.do(t, e.as(a, authz.RoleSalesAgent), http.MethodGet, path, nil).status)

	var page struct {
		Items []any `json:"items"`
	}
	e.do(t, e.as(b, authz.RoleCollections), http.MethodGet, "/api/v1/billing/receivables", nil).into(t, &page)
	assert.Empty(t, page.Items, "tenant B no ve la cartera de A")
}

func TestEndorsementAndCancellationEvents(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	reader := e.as(tenant, authz.RoleCollections)
	policy := e.issue(t, tenant, "monthly_installments", "300", "2026-01-01", "2026-03-31")
	path := "/api/v1/billing/plans/" + policy.String()

	require.NoError(t, e.deliver(t, tenant, policies.TopicPolicyEndorsed, policies.PolicyEndorsed{
		PolicyID: policy, EndorsementID: uuid.Must(uuid.NewV7()), Currency: "COP", NetPremiumDelta: "90", TaxesDelta: "10", EffectiveDate: "2026-02-01",
	}))
	var p plan
	e.do(t, reader, http.MethodGet, path, nil).into(t, &p)
	assert.Equal(t, "400", p.Total)
	assert.Equal(t, "133.34", p.Installments[2].Amount)

	require.NoError(t, e.deliver(t, tenant, policies.TopicPolicyCancelled, policies.PolicyCancelled{
		PolicyID: policy, Cause: "client_request", EffectiveDate: "2026-02-01", Currency: "COP", UnearnedPremium: "70",
	}))
	e.do(t, reader, http.MethodGet, path, nil).into(t, &p)
	assert.True(t, p.Cancelled)
	assert.Equal(t, "70", p.RefundAmount)
	assert.Equal(t, "voided", p.Installments[0].Status)

	// Sin plan (billing apagado al expedir): el consumidor no falla.
	require.NoError(t, e.deliver(t, tenant, policies.TopicPolicyCancelled, policies.PolicyCancelled{
		PolicyID: uuid.Must(uuid.NewV7()), EffectiveDate: "2026-02-01", Currency: "COP", UnearnedPremium: "0",
	}))
}

// Recuerda 5 días antes y el día del vencimiento, solo cuotas pendientes (DECISIONS T1).
func TestInstallmentReminders(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	today := kernel.Today(e.clock)
	e.issue(t, tenant, "single_payment", "100", today.String(), today.AddDays(300).String())
	e.issue(t, tenant, "single_payment", "100", today.AddDays(5).String(), today.AddDays(305).String())
	e.issue(t, tenant, "single_payment", "100", today.AddDays(6).String(), today.AddDays(306).String())
	require.NoError(t, app.SendInstallmentReminders(tenantCtx(tenant), e.store, e.clock))
	conn, err := pgx.Connect(context.Background(), superURL)
	require.NoError(t, err)
	defer conn.Close(context.Background()) //nolint:errcheck // fin de la prueba
	rows, err := conn.Query(context.Background(), `SELECT payload->>'daysLeft' FROM messaging.outbox WHERE tenant_id = $1 AND topic = 'billing.installment_due' ORDER BY 1`, uuid.UUID(tenant))
	require.NoError(t, err)
	days, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	assert.Equal(t, []string{"0", "5"}, days)
}

func TestOverdueUsesGraceDaysAndNoticesOnce(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	admin, reader := e.as(tenant, authz.RoleTenantAdmin), e.as(tenant, authz.RoleCollections)
	due := kernel.Today(e.clock).AddDays(-10)
	policy := e.issue(t, tenant, "single_payment", "100", due.String(), due.AddDays(300).String())
	path := "/api/v1/billing/plans/" + policy.String()
	ctx := tenantCtx(tenant)

	require.NoError(t, app.MarkOverdue(ctx, e.store, e.clock))
	assert.Zero(t, notified(t, tenant), "dentro de los 30 días por defecto")

	r := e.do(t, admin, http.MethodPut, "/api/v1/billing/settings/arrears", map[string]any{"graceDays": 5})
	require.Equal(t, http.StatusNoContent, r.status, string(r.raw))
	require.NoError(t, app.MarkOverdue(ctx, e.store, e.clock))
	require.NoError(t, app.MarkOverdue(ctx, e.store, e.clock))
	assert.Equal(t, 1, notified(t, tenant))

	var p plan
	e.do(t, reader, http.MethodGet, path, nil).into(t, &p)
	assert.True(t, p.InArrears)

	var summary []struct {
		Bucket  string `json:"bucket"`
		Balance string `json:"balance"`
	}
	e.do(t, reader, http.MethodGet, "/api/v1/billing/receivables/summary", nil).into(t, &summary)
	require.Len(t, summary, 1)
	assert.Equal(t, "d0_30", summary[0].Bucket)
	assert.Equal(t, "100", summary[0].Balance)

	var page struct {
		Items []struct {
			HolderName  string `json:"holderName"`
			DaysOverdue int    `json:"daysOverdue"`
		} `json:"items"`
	}
	e.do(t, reader, http.MethodGet, "/api/v1/billing/receivables?bucket=d0_30", nil).into(t, &page)
	require.Len(t, page.Items, 1)
	assert.Equal(t, 10, page.Items[0].DaysOverdue)
	assert.Equal(t, "Ana", page.Items[0].HolderName)
	e.do(t, reader, http.MethodGet, "/api/v1/billing/receivables?bucket=d31_60", nil).into(t, &page)
	assert.Empty(t, page.Items)
}

func TestAdjustScheduleChecksVersionAndSum(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	ops := e.as(tenant, authz.RoleOperations)
	policy := e.issue(t, tenant, "monthly_installments", "300", "2026-01-01", "2026-03-31")
	path := "/api/v1/billing/plans/" + policy.String() + "/installments"
	body := func(version int, amount string) map[string]any {
		return map[string]any{"version": version, "supportReference": "OF-1", "installments": []map[string]any{{"dueDate": "2026-05-01", "amount": amount}}}
	}
	assert.Equal(t, http.StatusConflict, e.do(t, ops, http.MethodPut, path, body(9, "300")).status)
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, ops, http.MethodPut, path, body(1, "299")).status)
	r := e.do(t, ops, http.MethodPut, path, body(1, "300"))
	require.Equal(t, http.StatusOK, r.status, string(r.raw))
	var p plan
	r.into(t, &p)
	assert.Equal(t, 2, p.Version)
	assert.Len(t, p.Installments, 1)
}

func TestFuturePaymentIsRejected(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	policy := e.issue(t, tenant, "single_payment", "100", "2026-01-01", "2026-12-31")
	body := paymentBody("100")
	body["paidOn"] = kernel.Today(e.clock).AddDays(1).String()
	r := e.do(t, e.as(tenant, authz.RoleCollections), http.MethodPost, "/api/v1/billing/plans/"+policy.String()+"/payments", body, "Idempotency-Key", "k")
	assert.Equal(t, http.StatusUnprocessableEntity, r.status)
}

func TestPaymentBeforeThePolicyWindowIsRejected(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	policy := e.issue(t, tenant, "single_payment", "100", "2026-03-01", "2026-12-31")
	body := paymentBody("100")
	body["paidOn"] = "2025-12-30" // un día antes de la ventana de 60 días (N22)
	r := e.do(t, e.as(tenant, authz.RoleCollections), http.MethodPost, "/api/v1/billing/plans/"+policy.String()+"/payments", body, "Idempotency-Key", "k")
	assert.Equal(t, http.StatusUnprocessableEntity, r.status)
}

func TestPaymentWritesFromAnotherTenantAre404(t *testing.T) {
	e := newEnv(t)
	a, b := newTenant(), newTenant()
	policy := e.issue(t, a, "single_payment", "100", "2026-01-01", "2026-12-31")
	base := "/api/v1/billing/plans/" + policy.String()
	r := e.do(t, e.as(a, authz.RoleCollections), http.MethodPost, base+"/payments", paymentBody("100"), "Idempotency-Key", "k")
	require.Equal(t, http.StatusCreated, r.status)
	var pay struct {
		ID string `json:"id"`
	}
	r.into(t, &pay)
	other := e.as(b, authz.RoleCollections)
	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodPost, base+"/payments", paymentBody("100"), "Idempotency-Key", "k2").status)
	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodPost, base+"/payments/"+pay.ID+"/reverse", map[string]any{"reason": "no es mío"}).status)
}
