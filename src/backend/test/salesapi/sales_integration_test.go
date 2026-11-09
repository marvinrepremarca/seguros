//go:build integration

// Package salesapi prueba CRM, cotización y siniestros de punta a punta contra PostgreSQL real.
package salesapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	catalog "github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/claims"
	claimshttp "github.com/marvinrepremarca/seguros/internal/modules/claims/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/crm"
	crmhttp "github.com/marvinrepremarca/seguros/internal/modules/crm/adapters/http"
	identity "github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	policiesmod "github.com/marvinrepremarca/seguros/internal/modules/policies"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/quoting"
	quotinghttp "github.com/marvinrepremarca/seguros/internal/modules/quoting/adapters/http"
	quotingevents "github.com/marvinrepremarca/seguros/internal/modules/quoting/contracts"
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
	if super, err = pgx.Connect(ctx, env.SuperuserURL); err != nil {
		return 1
	}
	return m.Run()
}

type principalKey struct{}

type checker struct{ off map[kernel.TenantID]bool }

func (c checker) IsEnabled(ctx context.Context, _ flags.Key) bool {
	t, ok := reqctx.TenantID(ctx)
	return ok && !c.off[t]
}

// fakes de los otros módulos: esta suite prueba las fronteras, no a sus dueños.
type world struct {
	visible map[uuid.UUID][]uuid.UUID // asesor → clientes
	facts   map[uuid.UUID]policies.PolicyFacts
}

func (w *world) VisibleToAgent(_ context.Context, party, agent uuid.UUID) (bool, error) {
	return slices.Contains(w.visible[agent], party), nil
}

func (w *world) Summaries(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]parties.PartySummary, error) {
	out := map[uuid.UUID]parties.PartySummary{}
	for _, id := range ids {
		out[id] = parties.PartySummary{ID: id, Name: "Ana"}
	}
	return out, nil
}

func (w *world) Facts(_ context.Context, id uuid.UUID) (policies.PolicyFacts, bool, error) {
	f, ok := w.facts[id]
	return f, ok, nil
}

func (w *world) IDsOfAgent(_ context.Context, agent uuid.UUID, _ int32) ([]uuid.UUID, error) {
	out := []uuid.UUID{}
	for id, f := range w.facts {
		if f.AgentID.Valid && f.AgentID.UUID == agent {
			out = append(out, id)
		}
	}
	return out, nil
}

type users struct{}

func (users) Agents(context.Context) ([]identity.User, error) { return nil, nil }

func (users) Agent(_ context.Context, id uuid.UUID) (identity.User, bool, error) {
	return identity.User{ID: id, Active: true}, true, nil
}

type cat struct{}

func (cat) ResolvePolicyRef(context.Context, catalog.PolicyRef) (string, error) {
	return "generic", nil
}

func (cat) RiskSchemaKeyOfLine(context.Context, uuid.UUID) (string, error) { return "generic", nil }

func (cat) AgreementFor(context.Context, catalog.PolicyRef, kernel.Date) (catalog.Agreement, bool, error) {
	return catalog.Agreement{}, false, nil
}

type schemas struct{}

func (schemas) List() []catalog.RiskSchema    { return nil }
func (schemas) Current(string) (int32, bool)  { return 1, true }
func (schemas) Validate(string, []byte) error { return nil }

// cipher invierte los bytes: suficiente para comprobar que no se guarda el texto plano.
type cipher struct{}

func reversed(b []byte) []byte {
	out := slices.Clone(b)
	slices.Reverse(out)
	return out
}

func (cipher) EncryptFor(_ context.Context, _, _ string, p []byte) ([]byte, error) {
	return reversed(p), nil
}
func (cipher) DecryptFor(_ context.Context, _, _ string, c []byte) ([]byte, error) {
	return reversed(c), nil
}

type env struct {
	router *chi.Mux
	policy *authz.Policy
	clock  *kernel.FakeClock
	off    map[kernel.TenantID]bool
	w      *world
	crmC   []outbox.Consumer
	polC   []outbox.Consumer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.NewFakeClock(time.Now().UTC()) //nolint:forbidigo // partición de auditoría del mes actual
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	e := &env{policy: policy, clock: clock, off: map[kernel.TenantID]bool{}, w: &world{visible: map[uuid.UUID][]uuid.UUID{}, facts: map[uuid.UUID]policies.PolicyFacts{}}}
	recorder, writer, ck := audit.NewRecorder(clock), outbox.NewWriter(clock), checker{off: e.off}
	c := crm.New(crm.Deps{DB: database, Audit: recorder, Scope: e.w, Users: users{}, Checker: ck, Policy: policy, Clock: clock, Logger: logger})
	q := quoting.New(quoting.Deps{DB: database, Audit: recorder, Outbox: writer, Scope: e.w, Parties: e.w, Users: users{}, Catalog: cat{}, Schemas: schemas{}, Lines: cat{}, Checker: ck, Policy: policy, Clock: clock, Logger: logger})
	cl := claims.New(claims.Deps{DB: database, Audit: recorder, Outbox: writer, Policies: e.w, Parties: e.w, Cipher: cipher{}, Checker: ck, Policy: policy, Clock: clock, Logger: logger})
	e.crmC = append(crm.Consumers(database, recorder, clock), quoting.Consumers(recorder)...)
	e.polC = policiesmod.Consumers(database, recorder, writer, clock)
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
		mountErr = errors.Join(c.Mount(r), q.Mount(r), cl.Mount(r))
	})
	require.NoError(t, mountErr)
	e.router = root
	return e
}

func newTenant() kernel.TenantID { return kernel.TenantID(uuid.Must(uuid.NewV7())) }

func (e *env) as(tenant kernel.TenantID, roles ...authz.Role) authz.Principal {
	return authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), tenant, roles, e.policy)
}

func userOf(p authz.Principal) uuid.UUID { return uuid.UUID(p.UserID) }

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
	contracttest.New(t, crmhttp.GetSpec, quotinghttp.GetSpec, claimshttp.GetSpec).Check(t, req, rec)
	if out != nil && rec.Code < 300 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out), rec.Body.String())
	}
	return rec.Code
}

func events(t *testing.T, tenant kernel.TenantID, topic string) []outbox.Event {
	t.Helper()
	rows, err := super.Query(context.Background(), `SELECT id, payload FROM messaging.outbox WHERE tenant_id = $1 AND topic = $2 ORDER BY id`, uuid.UUID(tenant), topic)
	require.NoError(t, err)
	defer rows.Close()
	var out []outbox.Event
	for rows.Next() {
		ev := outbox.Event{TenantID: tenant, Topic: topic, OccurredAt: time.Now().UTC()} //nolint:forbidigo // instante informativo
		require.NoError(t, rows.Scan(&ev.ID, &ev.Payload))
		out = append(out, ev)
	}
	return out
}

func deliver(t *testing.T, consumers []outbox.Consumer, name string, ev outbox.Event) {
	t.Helper()
	ctx := reqctx.WithTenantID(context.Background(), ev.TenantID)
	for _, c := range consumers {
		if c.Name == name {
			require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error { return c.Handle(ctx, tx, ev) }))
			return
		}
	}
	t.Fatalf("sin consumidor %s", name)
}

type lead struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	AgentID string `json:"agentId"`
	PartyID string `json:"partyId"`
}

type page[T any] struct {
	Items []T `json:"items"`
}

func newLeadBody() map[string]any {
	return map[string]any{"name": "Ana Díaz", "phone": "3001234567", "source": "referral"}
}

func TestLeadLifecycleAndScope(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	agent, other, mgr := e.as(tenant, authz.RoleSalesAgent), e.as(tenant, authz.RoleSalesAgent), e.as(tenant, authz.RoleManager)
	var l lead
	require.Equal(t, http.StatusCreated, e.do(t, agent, http.MethodPost, "/api/v1/crm/leads", newLeadBody(), &l))
	assert.Equal(t, userOf(agent).String(), l.AgentID, "queda como asesor de lo que registra")
	base := "/api/v1/crm/leads/" + l.ID

	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodGet, base, nil, nil), "otro asesor")
	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodPost, base+"/contacted", nil, nil))
	assert.Equal(t, http.StatusForbidden, e.do(t, mgr, http.MethodPost, base+"/contacted", nil, nil), "gerente solo lee y asigna")
	assert.Equal(t, http.StatusOK, e.do(t, mgr, http.MethodGet, base, nil, nil))
	var list page[lead]
	require.Equal(t, http.StatusOK, e.do(t, other, http.MethodGet, "/api/v1/crm/leads", nil, &list))
	assert.Empty(t, list.Items)

	assert.Equal(t, http.StatusConflict, e.do(t, agent, http.MethodPost, base+"/interested", nil, nil), "se salta un estado")
	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodPost, base+"/contacted", nil, nil))
	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodPost, base+"/interested", nil, nil))

	party := uuid.Must(uuid.NewV7())
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, agent, http.MethodPost, base+"/start-quoting", map[string]any{"partyId": party}, nil), "cliente fuera de su alcance")
	e.w.visible[userOf(agent)] = []uuid.UUID{party}
	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodPost, base+"/start-quoting", map[string]any{"partyId": party}, &l))
	assert.Equal(t, "quoting", l.Status)

	require.Equal(t, http.StatusOK, e.do(t, mgr, http.MethodPost, base+"/assign", map[string]any{"agentId": userOf(other)}, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, agent, http.MethodGet, base, nil, nil), "el anterior pierde el acceso")
	assert.Equal(t, http.StatusOK, e.do(t, other, http.MethodGet, base, nil, nil))
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, other, http.MethodPost, base+"/lost", map[string]any{"reason": "   "}, nil))
	require.Equal(t, http.StatusOK, e.do(t, other, http.MethodPost, base+"/lost", map[string]any{"reason": "No quiere"}, nil))
	assert.Equal(t, http.StatusConflict, e.do(t, mgr, http.MethodPost, base+"/assign", map[string]any{"agentId": userOf(agent)}, nil), "terminado")
}

func TestModulesFlagOffAndTenantIsolation(t *testing.T) {
	e := newEnv(t)
	a, b, off := newTenant(), newTenant(), newTenant()
	e.off[off] = true
	agent := e.as(a, authz.RoleSalesAgent)
	var l lead
	require.Equal(t, http.StatusCreated, e.do(t, agent, http.MethodPost, "/api/v1/crm/leads", newLeadBody(), &l))

	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(b, authz.RoleManager), http.MethodGet, "/api/v1/crm/leads/"+l.ID, nil, nil), "otro tenant")
	var list page[lead]
	require.Equal(t, http.StatusOK, e.do(t, e.as(b, authz.RoleManager), http.MethodGet, "/api/v1/crm/leads", nil, &list))
	assert.Empty(t, list.Items)

	for _, path := range []string{"/api/v1/crm/leads", "/api/v1/quoting/requests", "/api/v1/claims/claims"} {
		assert.Equal(t, http.StatusNotFound, e.do(t, e.as(off, authz.RoleManager), http.MethodGet, path, nil, nil), path)
		assert.Equal(t, http.StatusForbidden, e.do(t, e.as(a, authz.RoleAccounting), http.MethodGet, path, nil, nil), path)
	}
	assert.Equal(t, http.StatusBadRequest, e.do(t, e.as(a, authz.RoleManager), http.MethodGet, "/api/v1/crm/leads?limit=0", nil, nil))
}

type offer struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Expired bool   `json:"expired"`
}

type request struct {
	ID       string  `json:"id"`
	Status   string  `json:"status"`
	PolicyID string  `json:"policyId"`
	Offers   []offer `json:"offers"`
}

func offerBody(validUntil string) map[string]any {
	body := map[string]any{
		"insurerId": uuid.Must(uuid.NewV7()), "currency": "COP", "netPremium": "1000000", "taxes": "190000",
		"effectiveFrom": "2027-01-01", "effectiveTo": "2027-12-31",
		"coverages": []map[string]any{{"name": "Daños", "sumInsured": "50000000", "deductible": "10 %"}},
	}
	if validUntil != "" {
		body["validUntil"] = validUntil
	}
	return body
}

// Solicitud → ofertas → aceptación → QuoteAccepted → póliza EnTramite y prospecto ganado (Flujos 1–3).
func TestQuoteToPolicy(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	agent, other := e.as(tenant, authz.RoleSalesAgent), e.as(tenant, authz.RoleSalesAgent)
	party := uuid.Must(uuid.NewV7())
	e.w.visible[userOf(agent)] = []uuid.UUID{party}

	var l lead
	require.Equal(t, http.StatusCreated, e.do(t, agent, http.MethodPost, "/api/v1/crm/leads", newLeadBody(), &l))
	lb := "/api/v1/crm/leads/" + l.ID
	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodPost, lb+"/contacted", nil, nil))
	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodPost, lb+"/interested", nil, nil))
	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodPost, lb+"/start-quoting", map[string]any{"partyId": party}, nil))

	create := map[string]any{"leadId": l.ID, "holderPartyId": party, "lineOfBusinessId": uuid.Must(uuid.NewV7()), "riskDescription": "Auto ABC123", "riskData": map[string]any{"note": "x"}}
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, other, http.MethodPost, "/api/v1/quoting/requests", create, nil), "cliente ajeno")
	var r request
	require.Equal(t, http.StatusCreated, e.do(t, agent, http.MethodPost, "/api/v1/quoting/requests", create, &r))
	base := "/api/v1/quoting/requests/" + r.ID
	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodGet, base, nil, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(newTenant(), authz.RoleManager), http.MethodGet, base, nil, nil))

	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodPost, base+"/offers", offerBody(""), &r))
	today := kernel.Today(e.clock)
	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodPost, base+"/offers", offerBody(today.String()), &r))
	require.Len(t, r.Offers, 2)
	loser, winner := r.Offers[0], r.Offers[1]

	e.clock.Advance(48 * time.Hour)
	assert.Equal(t, http.StatusConflict, e.do(t, agent, http.MethodPost, base+"/accept", map[string]any{"offerId": winner.ID, "paymentForm": "single_payment"}, nil), "oferta vencida")
	assert.Empty(t, events(t, tenant, quotingevents.TopicQuoteAccepted))

	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodPost, base+"/accept", map[string]any{"offerId": loser.ID, "paymentForm": "monthly_installments"}, &r))
	assert.Equal(t, "accepted", r.Status)
	assert.Equal(t, http.StatusConflict, e.do(t, agent, http.MethodPost, base+"/accept", map[string]any{"offerId": loser.ID, "paymentForm": "single_payment"}, nil), "no se acepta dos veces")

	got := events(t, tenant, quotingevents.TopicQuoteAccepted)
	require.Len(t, got, 1)
	deliver(t, e.polC, "policies.quote-accepted", got[0])
	deliver(t, e.crmC, "crm.quote-accepted", got[0])

	ctx := reqctx.WithTenantID(context.Background(), tenant)
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		var status, form, net string
		var parties int
		err := tx.QueryRow(ctx, `SELECT p.status, p.payment_form, p.net_premium::text, (SELECT count(*) FROM policies.policy_parties x WHERE x.policy_id = p.id) FROM policies.policies p WHERE p.id = $1`, r.PolicyID).Scan(&status, &form, &net, &parties)
		require.NoError(t, err)
		assert.Equal(t, "in_process", status)
		assert.Equal(t, "monthly_installments", form)
		assert.Equal(t, "1000000.0000", net)
		assert.Equal(t, 2, parties, "tomador y, por defecto, él mismo como asegurado")
		return nil
	}))
	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodGet, lb, nil, &l))
	assert.Equal(t, "won", l.Status)
}

// Un leadId ajeno en la solicitud no marca como ganado el prospecto de otro asesor.
func TestAcceptedQuoteDoesNotWinForeignLead(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	owner, attacker := e.as(tenant, authz.RoleSalesAgent), e.as(tenant, authz.RoleSalesAgent)
	party := uuid.Must(uuid.NewV7())
	e.w.visible[userOf(owner)] = []uuid.UUID{party}
	var l lead
	require.Equal(t, http.StatusCreated, e.do(t, owner, http.MethodPost, "/api/v1/crm/leads", newLeadBody(), &l))
	lb := "/api/v1/crm/leads/" + l.ID
	require.Equal(t, http.StatusOK, e.do(t, owner, http.MethodPost, lb+"/contacted", nil, nil))
	require.Equal(t, http.StatusOK, e.do(t, owner, http.MethodPost, lb+"/interested", nil, nil))
	require.Equal(t, http.StatusOK, e.do(t, owner, http.MethodPost, lb+"/start-quoting", map[string]any{"partyId": party}, nil))
	leadID := uuid.MustParse(l.ID)
	agentID := userOf(attacker)
	raw, err := json.Marshal(quotingevents.QuoteAccepted{LeadID: &leadID, AgentID: &agentID, HolderPartyID: party})
	require.NoError(t, err)
	deliver(t, e.crmC, "crm.quote-accepted", outbox.Event{ID: uuid.Must(uuid.NewV7()), TenantID: tenant, Payload: raw})
	require.Equal(t, http.StatusOK, e.do(t, owner, http.MethodGet, lb, nil, &l))
	assert.Equal(t, "quoting", l.Status)
}

func TestQuotingSettings(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	admin := e.as(tenant, authz.RoleTenantAdmin)
	var cfg struct {
		OfferValidDays int `json:"offerValidDays"`
	}
	require.Equal(t, http.StatusOK, e.do(t, admin, http.MethodGet, "/api/v1/quoting/settings", nil, &cfg))
	assert.Equal(t, 30, cfg.OfferValidDays, "T3")
	assert.Equal(t, http.StatusForbidden, e.do(t, e.as(tenant, authz.RoleSalesAgent), http.MethodPut, "/api/v1/quoting/settings", map[string]any{"offerValidDays": 5}, nil))
	require.Equal(t, http.StatusNoContent, e.do(t, admin, http.MethodPut, "/api/v1/quoting/settings", map[string]any{"offerValidDays": 5}, nil))
	require.Equal(t, http.StatusOK, e.do(t, admin, http.MethodGet, "/api/v1/quoting/settings", nil, &cfg))
	assert.Equal(t, 5, cfg.OfferValidDays)
	assert.Equal(t, http.StatusOK, e.do(t, e.as(newTenant(), authz.RoleTenantAdmin), http.MethodGet, "/api/v1/quoting/settings", nil, &cfg))
	assert.Equal(t, 30, cfg.OfferValidDays, "otro tenant no comparte el parámetro")
}

type claim struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	AlertOutOfCover  bool   `json:"alertOutOfCoverage"`
	AlertInArrears   bool   `json:"alertInArrears"`
	AlertNotInForce  bool   `json:"alertNotInForce"`
	HasHealthDetails *bool  `json:"hasHealthDetails"`
	PaidAmount       string `json:"paidAmount"`
}

func (e *env) policyOf(t *testing.T, agent uuid.UUID, status string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	from, err := kernel.ParseDate("2026-01-01")
	require.NoError(t, err)
	to, err := kernel.ParseDate("2026-12-31")
	require.NoError(t, err)
	e.w.facts[id] = policies.PolicyFacts{ID: id, Status: status, Number: "P-1", InsurerID: uuid.Must(uuid.NewV7()), HolderPartyID: uuid.Must(uuid.NewV7()), AgentID: uuid.NullUUID{UUID: agent, Valid: true}, Currency: "COP", EffectiveFrom: from, EffectiveTo: to}
	return id
}

func reportBody(policy uuid.UUID, occurred string) map[string]any {
	return map[string]any{"policyId": policy, "occurredOn": occurred, "location": "Bogotá", "claimType": "Choque", "description": "Choque simple", "claimedAmount": "1000"}
}

func TestClaimLifecycle(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	e.clock.Set(time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC))
	agent, other, analyst, mgr := e.as(tenant, authz.RoleSalesAgent), e.as(tenant, authz.RoleSalesAgent), e.as(tenant, authz.RoleClaimsAnalyst), e.as(tenant, authz.RoleManager)
	p := e.policyOf(t, userOf(agent), "in_arrears")

	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodPost, "/api/v1/claims/claims", reportBody(p, "2026-06-01"), nil), "póliza de otro asesor")
	assert.Equal(t, http.StatusNotFound, e.do(t, agent, http.MethodPost, "/api/v1/claims/claims", reportBody(uuid.Must(uuid.NewV7()), "2026-06-01"), nil))
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, agent, http.MethodPost, "/api/v1/claims/claims", reportBody(p, "2027-01-01"), nil), "ocurrencia futura")
	var c claim
	require.Equal(t, http.StatusCreated, e.do(t, agent, http.MethodPost, "/api/v1/claims/claims", reportBody(p, "2025-06-01"), &c))
	assert.True(t, c.AlertOutOfCover, "alerta, no bloqueo")
	assert.True(t, c.AlertInArrears)
	assert.Len(t, events(t, tenant, "claims.claim_reported"), 1)
	base := "/api/v1/claims/claims/" + c.ID

	assert.Equal(t, http.StatusOK, e.do(t, agent, http.MethodGet, base, nil, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodGet, base, nil, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(newTenant(), authz.RoleManager), http.MethodGet, base, nil, nil))
	var list page[claim]
	require.Equal(t, http.StatusOK, e.do(t, other, http.MethodGet, "/api/v1/claims/claims", nil, &list))
	assert.Empty(t, list.Items)
	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodGet, "/api/v1/claims/claims", nil, &list))
	assert.Len(t, list.Items, 1)

	// Un traspaso mueve el acceso sin esperar a nada (DECISIONS N11).
	f := e.w.facts[p]
	f.AgentID = uuid.NullUUID{UUID: userOf(other), Valid: true}
	e.w.facts[p] = f
	assert.Equal(t, http.StatusNotFound, e.do(t, agent, http.MethodGet, base, nil, nil))
	assert.Equal(t, http.StatusOK, e.do(t, other, http.MethodGet, base, nil, nil))

	// El asesor no avanza, no ve gestiones ni salud.
	assert.Equal(t, http.StatusForbidden, e.do(t, other, http.MethodPost, base+"/status", map[string]any{"to": "notified"}, nil))
	assert.Equal(t, http.StatusForbidden, e.do(t, other, http.MethodGet, base+"/tracking", nil, nil))
	assert.Equal(t, http.StatusForbidden, e.do(t, other, http.MethodGet, base+"/health-details", nil, nil))

	advance := func(to, support string, want int) {
		t.Helper()
		body := map[string]any{"to": to}
		if support != "" {
			body["support"] = support
		}
		require.Equal(t, want, e.do(t, analyst, http.MethodPost, base+"/status", body, nil), to)
	}
	advance("evaluating", "", http.StatusConflict)
	advance("notified", "", http.StatusOK)
	advance("documenting", "", http.StatusOK)
	advance("evaluating", "", http.StatusOK)
	advance("approved", "", http.StatusUnprocessableEntity)
	advance("objected", "Carta 1", http.StatusOK)
	advance("evaluating", "", http.StatusOK)
	advance("approved", "Carta 2", http.StatusOK)

	// Tope de lo reclamado: solo Manager lo supera.
	assert.Equal(t, http.StatusConflict, e.do(t, analyst, http.MethodPost, base+"/payment", map[string]any{"amount": "1001"}, nil))
	over := map[string]any{"amount": "1001", "reason": "Gastos reconocidos", "support": "Carta 3"}
	assert.Equal(t, http.StatusForbidden, e.do(t, analyst, http.MethodPost, base+"/payment-override", over, nil))
	require.Equal(t, http.StatusOK, e.do(t, mgr, http.MethodPost, base+"/payment-override", over, &c))
	assert.Equal(t, "paid", c.Status)
	advance("closed", "", http.StatusOK)

	var tracking struct {
		Notes []struct {
			Body string `json:"body"`
		} `json:"notes"`
	}
	require.Equal(t, http.StatusOK, e.do(t, mgr, http.MethodGet, base+"/tracking", nil, &tracking))
	require.Len(t, tracking.Notes, 1)
	assert.Contains(t, tracking.Notes[0].Body, "Gastos reconocidos")
	assert.Len(t, events(t, tenant, "claims.claim_status_changed"), 8)
	var entries int
	require.NoError(t, super.QueryRow(context.Background(), `SELECT count(*) FROM audit.events WHERE tenant_id = $1 AND action = 'claims.claim.paid_override'`, uuid.UUID(tenant)).Scan(&entries))
	assert.Equal(t, 1, entries)
}

func TestHealthDetailsAreEncryptedAndAudited(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	e.clock.Set(time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC))
	agent, analyst, mgr := e.as(tenant, authz.RoleSalesAgent), e.as(tenant, authz.RoleClaimsAnalyst), e.as(tenant, authz.RoleManager)
	p := e.policyOf(t, userOf(agent), "active")
	var c claim
	require.Equal(t, http.StatusCreated, e.do(t, agent, http.MethodPost, "/api/v1/claims/claims", reportBody(p, "2026-06-01"), &c))
	base := "/api/v1/claims/claims/" + c.ID
	assert.Equal(t, http.StatusNotFound, e.do(t, analyst, http.MethodGet, base+"/health-details", nil, nil), "sin detalle")

	secret := "Fractura de fémur"
	assert.Equal(t, http.StatusForbidden, e.do(t, mgr, http.MethodPut, base+"/health-details", map[string]any{"details": secret}, nil), "gerente solo lee")
	require.Equal(t, http.StatusNoContent, e.do(t, analyst, http.MethodPut, base+"/health-details", map[string]any{"details": secret}, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(newTenant(), authz.RoleClaimsAnalyst), http.MethodGet, base+"/health-details", nil, nil))

	var stored []byte
	require.NoError(t, super.QueryRow(context.Background(), `SELECT health_ciphertext FROM claims.claims WHERE id = $1`, c.ID).Scan(&stored))
	assert.NotContains(t, string(stored), secret)

	var got struct {
		Details string `json:"details"`
	}
	require.Equal(t, http.StatusOK, e.do(t, mgr, http.MethodGet, base+"/health-details", nil, &got))
	assert.Equal(t, secret, got.Details)
	var reads int
	require.NoError(t, super.QueryRow(context.Background(), `SELECT count(*) FROM audit.events WHERE tenant_id = $1 AND action = 'claims.health.read'`, uuid.UUID(tenant)).Scan(&reads))
	assert.Equal(t, 1, reads)

	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodGet, base, nil, &c))
	assert.Nil(t, c.HasHealthDetails, "el asesor no sabe si existe")
	require.Equal(t, http.StatusOK, e.do(t, mgr, http.MethodGet, base, nil, &c))
	require.NotNil(t, c.HasHealthDetails)

	assert.NotContains(t, eventsPayload(t, tenant), secret)
}

func eventsPayload(t *testing.T, tenant kernel.TenantID) string {
	t.Helper()
	var all string
	require.NoError(t, super.QueryRow(context.Background(), `SELECT coalesce(string_agg(payload::text, ' '), '') FROM messaging.outbox WHERE tenant_id = $1`, uuid.UUID(tenant)).Scan(&all))
	return all
}

func TestPortfolioTransferMovesOpenLeads(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	from, to := e.as(tenant, authz.RoleSalesAgent), e.as(tenant, authz.RoleSalesAgent)
	var open, closed lead
	require.Equal(t, http.StatusCreated, e.do(t, from, http.MethodPost, "/api/v1/crm/leads", newLeadBody(), &open))
	require.Equal(t, http.StatusCreated, e.do(t, from, http.MethodPost, "/api/v1/crm/leads", newLeadBody(), &closed))
	require.Equal(t, http.StatusOK, e.do(t, from, http.MethodPost, "/api/v1/crm/leads/"+closed.ID+"/lost", map[string]any{"reason": "No quiere"}, nil))
	raw, err := json.Marshal(policies.PortfolioTransferRequested{RequestID: uuid.Must(uuid.NewV7()), FromAgentID: userOf(from), ToAgentID: userOf(to), Reason: kernel.TransferOther, RequestedBy: userOf(to), RequestedAt: e.clock.Now().Add(time.Second)})
	require.NoError(t, err)
	deliver(t, e.crmC, "crm.portfolio-transfer", outbox.Event{ID: uuid.Must(uuid.NewV7()), TenantID: tenant, OccurredAt: e.clock.Now(), Payload: raw})
	assert.Equal(t, http.StatusOK, e.do(t, to, http.MethodGet, "/api/v1/crm/leads/"+open.ID, nil, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, from, http.MethodGet, "/api/v1/crm/leads/"+open.ID, nil, nil))
	assert.Equal(t, http.StatusOK, e.do(t, from, http.MethodGet, "/api/v1/crm/leads/"+closed.ID, nil, nil), "lo perdido no se mueve")
}

// El asesor anterior deja de ver las solicitudes abiertas y el nuevo las gestiona (DECISIONS N11).
func TestPortfolioTransferMovesOpenRequests(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	from, to := e.as(tenant, authz.RoleSalesAgent), e.as(tenant, authz.RoleSalesAgent)
	party := uuid.Must(uuid.NewV7())
	e.w.visible[userOf(from)] = []uuid.UUID{party}
	create := map[string]any{"holderPartyId": party, "lineOfBusinessId": uuid.Must(uuid.NewV7()), "riskDescription": "Auto", "riskData": map[string]any{}}
	var r request
	require.Equal(t, http.StatusCreated, e.do(t, from, http.MethodPost, "/api/v1/quoting/requests", create, &r))
	raw, err := json.Marshal(policies.PortfolioTransferRequested{RequestID: uuid.Must(uuid.NewV7()), FromAgentID: userOf(from), ToAgentID: userOf(to), Reason: kernel.TransferOther, RequestedBy: userOf(to), RequestedAt: e.clock.Now().Add(time.Second)})
	require.NoError(t, err)
	deliver(t, e.crmC, "quoting.portfolio-transfer", outbox.Event{ID: uuid.Must(uuid.NewV7()), TenantID: tenant, OccurredAt: e.clock.Now(), Payload: raw})
	assert.Equal(t, http.StatusNotFound, e.do(t, from, http.MethodGet, "/api/v1/quoting/requests/"+r.ID, nil, nil))
	assert.Equal(t, http.StatusOK, e.do(t, to, http.MethodGet, "/api/v1/quoting/requests/"+r.ID, nil, nil))
}

// Asegurados repetidos no pueden producir un evento que Policies rechace.
func TestAcceptDeduplicatesInsured(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	agent := e.as(tenant, authz.RoleSalesAgent)
	party := uuid.Must(uuid.NewV7())
	e.w.visible[userOf(agent)] = []uuid.UUID{party}
	var r request
	require.Equal(t, http.StatusCreated, e.do(t, agent, http.MethodPost, "/api/v1/quoting/requests", map[string]any{"holderPartyId": party, "lineOfBusinessId": uuid.Must(uuid.NewV7()), "riskDescription": "Auto", "riskData": map[string]any{}}, &r))
	base := "/api/v1/quoting/requests/" + r.ID
	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodPost, base+"/offers", offerBody(""), &r))
	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodPost, base+"/accept", map[string]any{"offerId": r.Offers[0].ID, "paymentForm": "single_payment", "insuredPartyIds": []uuid.UUID{party, party}}, &r))
	got := events(t, tenant, quotingevents.TopicQuoteAccepted)
	require.Len(t, got, 1)
	deliver(t, e.polC, "policies.quote-accepted", got[0])
}

// El asesor consulta el estado, no el soporte de la decisión (DECISIONS N29).
func TestAgentDoesNotSeeDecisionSupport(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	e.clock.Set(time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC))
	agent, analyst := e.as(tenant, authz.RoleSalesAgent), e.as(tenant, authz.RoleClaimsAnalyst)
	var c claim
	require.Equal(t, http.StatusCreated, e.do(t, agent, http.MethodPost, "/api/v1/claims/claims", reportBody(e.policyOf(t, userOf(agent), "active"), "2026-06-01"), &c))
	base := "/api/v1/claims/claims/" + c.ID
	for _, to := range []string{"notified", "documenting", "evaluating"} {
		require.Equal(t, http.StatusOK, e.do(t, analyst, http.MethodPost, base+"/status", map[string]any{"to": to}, nil))
	}
	require.Equal(t, http.StatusOK, e.do(t, analyst, http.MethodPost, base+"/status", map[string]any{"to": "objected", "support": "Carta 7"}, nil))
	var got map[string]any
	require.Equal(t, http.StatusOK, e.do(t, agent, http.MethodGet, base, nil, &got))
	assert.NotContains(t, got, "decisionSupport")
	require.Equal(t, http.StatusOK, e.do(t, analyst, http.MethodGet, base, nil, &got))
	assert.Equal(t, "Carta 7", got["decisionSupport"])
}

// Cambiar lo reclamado exige motivo: de ese valor depende el tope del pago (DECISIONS N32).
func TestClaimedAmountRequiresReason(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	e.clock.Set(time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC))
	agent, analyst := e.as(tenant, authz.RoleSalesAgent), e.as(tenant, authz.RoleClaimsAnalyst)
	var c claim
	require.Equal(t, http.StatusCreated, e.do(t, agent, http.MethodPost, "/api/v1/claims/claims", reportBody(e.policyOf(t, userOf(agent), "active"), "2026-06-01"), &c))
	path := "/api/v1/claims/claims/" + c.ID + "/claimed-amount"
	assert.Equal(t, http.StatusBadRequest, e.do(t, analyst, http.MethodPost, path, map[string]any{"amount": "2000"}, nil))
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, analyst, http.MethodPost, path, map[string]any{"amount": "2000", "reason": "    "}, nil))
	require.Equal(t, http.StatusOK, e.do(t, analyst, http.MethodPost, path, map[string]any{"amount": "2000", "reason": "Factura nueva"}, nil))
	var tracking struct {
		Notes []struct {
			Body string `json:"body"`
		} `json:"notes"`
	}
	require.Equal(t, http.StatusOK, e.do(t, analyst, http.MethodGet, "/api/v1/claims/claims/"+c.ID+"/tracking", nil, &tracking))
	require.Len(t, tracking.Notes, 1)
	assert.Contains(t, tracking.Notes[0].Body, "Factura nueva")
}
