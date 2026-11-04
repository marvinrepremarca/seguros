//go:build integration

// Package policiesapi prueba la API de pólizas de punta a punta con los módulos reales de los que
// depende (parties, catalog); por eso vive fuera del módulo: cruza sus fronteras.
package policiesapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/catalog"
	cataloghttp "github.com/marvinrepremarca/seguros/internal/modules/catalog/adapters/http"
	identity "github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/parties"
	partiesevents "github.com/marvinrepremarca/seguros/internal/modules/parties/adapters/events"
	partieshttp "github.com/marvinrepremarca/seguros/internal/modules/parties/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/policies"
	policiesevents "github.com/marvinrepremarca/seguros/internal/modules/policies/adapters/events"
	policieshttp "github.com/marvinrepremarca/seguros/internal/modules/policies/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/app"
	policycontracts "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/crypto"
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
	workerDB *db.DB
	super    *pgx.Conn
	superMu  sync.Mutex
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
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer database.Close()
	if workerDB, err = env.OpenWorker(ctx); err != nil {
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer workerDB.Close()
	if super, err = pgx.Connect(ctx, env.SuperuserURL); err != nil {
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer super.Close(ctx) //nolint:errcheck // limpieza al final de la suite
	return m.Run()
}

type principalKey struct{}

// flagControl simula module.compliance por prueba: valor o falla de evaluación.
type flagControl struct {
	res flags.Resolution
	err error
}

func (f *flagControl) Resolve(context.Context, kernel.TenantID, string, flags.Key) (flags.Resolution, error) {
	return f.res, f.err
}

type users struct{ list []identity.User }

func (u *users) Agents(context.Context) ([]identity.User, error) { return u.list, nil }

type approvedKyc struct{ ok bool }

func (k *approvedKyc) IsApproved(context.Context, uuid.UUID) (bool, error) { return k.ok, nil }

type env struct {
	router  *chi.Mux
	policy  *authz.Policy
	flag    *flagControl
	kyc     *approvedKyc
	users   *users
	clock   *kernel.FakeClock
	parties *parties.Module
	catalog *catalog.Module
}

func newEnv(t *testing.T) *env {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.NewFakeClock(time.Now().UTC()) //nolint:forbidigo // partición de auditoría del mes actual
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	kek, err := crypto.NewLocalKEK(map[string]string{"v1": "kek-de-prueba-de-al-menos-32-caracteres"}, "v1")
	require.NoError(t, err)
	cipher, err := crypto.NewService(database, kek, "indice-de-prueba-de-al-menos-32-caracteres", clock)
	require.NoError(t, err)
	recorder := audit.NewRecorder(clock)
	e := &env{users: &users{}}
	partiesMod := parties.New(parties.Deps{DB: database, Audit: recorder, Cipher: cipher, Agents: e.users, Policy: policy, Clock: clock, Logger: logger})
	catalogMod, err := catalog.New(catalog.Deps{DB: database, Audit: recorder, Policy: policy, Clock: clock, Logger: logger})
	require.NoError(t, err)
	e.policy, e.clock, e.parties, e.catalog = policy, clock, partiesMod, catalogMod
	e.flag, e.kyc = &flagControl{res: flags.Resolution{Source: flags.SourceDefault}}, &approvedKyc{}
	mod := policies.New(policies.Deps{
		DB: database, Audit: recorder, Outbox: outbox.NewWriter(clock), Parties: partiesMod.Directory, Catalog: catalogMod.Directory,
		Schemas: catalogMod.Schemas, Users: e.users, Flags: e.flag, Kyc: e.kyc, Policy: policy, Clock: clock, Logger: logger,
	})
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
		mountErr = errors.Join(mod.Mount(r), partiesMod.Mount(r), catalogMod.Mount(r))
	})
	require.NoError(t, mountErr)
	e.router = root
	return e
}

func newTenant() kernel.TenantID { return kernel.TenantID(uuid.Must(uuid.NewV7())) }

func (e *env) as(tenant kernel.TenantID, roles ...authz.Role) *authz.Principal {
	p := authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), tenant, roles, e.policy)
	return &p
}

type resp struct {
	status int
	body   map[string]any
}

func (r resp) str(t *testing.T, key string) string {
	t.Helper()
	v, ok := r.body[key].(string)
	require.True(t, ok, "%s en %v", key, r.body)
	return v
}

func (r resp) version(t *testing.T) int {
	t.Helper()
	v, ok := r.body["version"].(float64)
	require.True(t, ok, "version en %v", r.body)
	return int(v)
}

func (e *env) do(t *testing.T, who *authz.Principal, method, path string, body any) resp {
	t.Helper()
	ctx := context.Background()
	if who != nil {
		ctx = context.WithValue(ctx, principalKey{}, *who)
	}
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
	contracttest.New(t, policieshttp.GetSpec, partieshttp.GetSpec, cataloghttp.GetSpec).Check(t, req, rec)
	out := resp{status: rec.Code, body: map[string]any{}}
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out.body), rec.Body.String())
	}
	return out
}

const base = "/api/v1/policies/policies"

// world crea un tenant con catálogo y terceros reales.
type world struct {
	tenant          kernel.TenantID
	ops             *authz.Principal
	insurer, line   string
	holder, insured string
}

func (e *env) world(t *testing.T) world {
	t.Helper()
	tenant := newTenant()
	admin := e.as(tenant, authz.RoleTenantAdmin)
	ops := e.as(tenant, authz.RoleOperations)
	create := func(who *authz.Principal, path string, body any) string {
		r := e.do(t, who, http.MethodPost, path, body)
		require.Equal(t, http.StatusCreated, r.status, r.body)
		return r.str(t, "id")
	}
	party := func(name string) string {
		return create(ops, "/api/v1/parties/parties", map[string]any{
			"kind": "person", "name": name, "documentType": "CC", "documentNumber": fmt.Sprintf("1%09d", uuid.New().ID()%1_000_000_000),
		})
	}
	return world{
		tenant: tenant, ops: ops,
		insurer: create(admin, "/api/v1/catalog/insurers", map[string]any{"name": "Sura"}),
		line:    create(admin, "/api/v1/catalog/lines-of-business", map[string]any{"name": "Autos", "riskSchemaKey": "auto"}),
		holder:  party("Ana Pérez"), insured: party("Luis Gómez"),
	}
}

func (w world) terms() map[string]any {
	return map[string]any{
		"insurerId": w.insurer, "lineOfBusinessId": w.line, "currency": "COP", "netPremium": "1000000", "taxes": "190000.50",
		"effectiveFrom": "2026-01-01", "effectiveTo": "2026-12-31", "paymentForm": "single_payment",
		"parties": []any{
			map[string]any{"partyId": w.holder, "role": "holder"},
			map[string]any{"partyId": w.insured, "role": "insured"},
		},
		"coverages": []any{map[string]any{"name": "Daños", "sumInsured": "50000000", "deductible": "10%"}},
		"items":     []any{map[string]any{"description": "Mazda 3", "riskData": map[string]any{"plate": "ABC123", "brand": "Mazda", "model": "3", "year": 2024}}},
	}
}

func (e *env) create(t *testing.T, w world) resp {
	t.Helper()
	r := e.do(t, w.ops, http.MethodPost, base, w.terms())
	require.Equal(t, http.StatusCreated, r.status, r.body)
	return r
}

func at(id string, version int, action string) string {
	return fmt.Sprintf("%s/%s/%s?version=%d", base, id, action, version)
}

// issued lleva una póliza hasta Vigente.
func (e *env) issued(t *testing.T, w world) resp {
	t.Helper()
	c := e.create(t, w)
	id := c.str(t, "id")
	a := e.do(t, w.ops, http.MethodPost, at(id, c.version(t), "accept"), nil)
	require.Equal(t, http.StatusOK, a.status, a.body)
	i := e.do(t, w.ops, http.MethodPost, at(id, a.version(t), "issue"), map[string]any{"policyNumber": "POL-" + uuid.NewString()[:8]})
	require.Equal(t, http.StatusOK, i.status, i.body)
	return i
}

func outboxTopics(t *testing.T, tenant kernel.TenantID) []string {
	t.Helper()
	superMu.Lock()
	defer superMu.Unlock()
	rows, err := super.Query(context.Background(), `SELECT topic FROM messaging.outbox WHERE tenant_id = $1 ORDER BY id`, uuid.UUID(tenant))
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	return out
}

func auditDetails(t *testing.T, tenant kernel.TenantID, action string) []map[string]string {
	t.Helper()
	ctx := reqctx.WithTenantID(context.Background(), tenant)
	var out []map[string]string
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT details FROM audit.events WHERE action = $1 ORDER BY id`, action)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d map[string]string
			if err := rows.Scan(&d); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	}))
	return out
}

func TestManualIssuanceFlow(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world(t)

	c := e.create(t, w)
	assert.Equal(t, "in_quotation", c.body["status"])
	assert.Equal(t, "1190000.5", c.body["totalPremium"], "neta + impuestos, sin pasar por float")
	id := c.str(t, "id")

	got := e.do(t, w.ops, http.MethodGet, base+"/"+id, nil)
	require.Equal(t, http.StatusOK, got.status)
	assert.Len(t, got.body["parties"], 2)

	early := e.do(t, w.ops, http.MethodPost, at(id, 1, "issue"), map[string]any{"policyNumber": "POL-1"})
	assert.Equal(t, http.StatusConflict, early.status)
	assert.Equal(t, "policies.policy.invalid_transition", early.body["type"])

	a := e.do(t, w.ops, http.MethodPost, at(id, 1, "accept"), nil)
	require.Equal(t, http.StatusOK, a.status, a.body)
	assert.Equal(t, "in_process", a.body["status"])

	stale := e.do(t, w.ops, http.MethodPost, at(id, 1, "issue"), map[string]any{"policyNumber": "POL-1"})
	assert.Equal(t, "policies.policy.concurrent_update", stale.body["type"])

	adjusted := w.terms()
	adjusted["netPremium"] = "1100000"
	u := e.do(t, w.ops, http.MethodPut, fmt.Sprintf("%s/%s?version=%d", base, id, a.version(t)), adjusted)
	require.Equal(t, http.StatusOK, u.status, u.body)
	assert.Equal(t, "1100000", u.body["netPremium"])

	i := e.do(t, w.ops, http.MethodPost, at(id, u.version(t), "issue"), map[string]any{"policyNumber": "POL-1"})
	require.Equal(t, http.StatusOK, i.status, i.body)
	assert.Equal(t, "active", i.body["status"])
	assert.Equal(t, "POL-1", i.body["policyNumber"])
	assert.Contains(t, outboxTopics(t, w.tenant), "policies.policy_issued")

	notEditable := e.do(t, w.ops, http.MethodPut, fmt.Sprintf("%s/%s?version=%d", base, id, i.version(t)), adjusted)
	assert.Equal(t, "policies.policy.not_editable", notEditable.body["type"])

	details := auditDetails(t, w.tenant, "policies.policy.issued")
	require.Len(t, details, 1)
	assert.Equal(t, "not_required", details[0]["kyc"])
	assert.Equal(t, "default", details[0]["complianceFlagSource"])
	assert.Equal(t, "in_process", details[0]["from"])

	// El número es único por aseguradora dentro del tenant.
	other := e.create(t, w)
	oa := e.do(t, w.ops, http.MethodPost, at(other.str(t, "id"), 1, "accept"), nil)
	dup := e.do(t, w.ops, http.MethodPost, at(other.str(t, "id"), oa.version(t), "issue"), map[string]any{"policyNumber": "POL-1"})
	assert.Equal(t, http.StatusConflict, dup.status)
	assert.Equal(t, "policies.policy.number_taken", dup.body["type"])
}

// ADR 0012 de punta a punta: la evaluación fallida da 503 y la póliza no cambia.
func TestIssueGate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		flag     flagControl
		approved bool
		status   int
		code     string
		kycAudit string
	}{
		{"flag apagado", flagControl{res: flags.Resolution{Source: flags.SourcePlan}}, false, http.StatusOK, "", "not_required"},
		{"flag encendido sin KYC", flagControl{res: flags.Resolution{Enabled: true, Source: flags.SourcePlan}}, false, http.StatusConflict, "policies.issue.kyc_not_approved", ""},
		{"flag encendido con KYC", flagControl{res: flags.Resolution{Enabled: true, Source: flags.SourcePlan}}, true, http.StatusOK, "", "approved"},
		{"flag no evaluable", flagControl{err: errors.New("valkey y postgres caídos")}, true, http.StatusServiceUnavailable, "policies.issue.compliance_unavailable", ""},
		{"flag con origen error", flagControl{res: flags.Resolution{Source: flags.SourceError}}, true, http.StatusServiceUnavailable, "policies.issue.compliance_unavailable", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			w := e.world(t)
			created := e.create(t, w)
			id := created.str(t, "id")
			a := e.do(t, w.ops, http.MethodPost, at(id, 1, "accept"), nil)
			require.Equal(t, http.StatusOK, a.status)

			*e.flag = c.flag
			e.kyc.ok = c.approved
			r := e.do(t, w.ops, http.MethodPost, at(id, a.version(t), "issue"), map[string]any{"policyNumber": "POL-1"})
			assert.Equal(t, c.status, r.status, r.body)

			got := e.do(t, w.ops, http.MethodGet, base+"/"+id, nil)
			if c.code != "" {
				assert.Equal(t, c.code, r.body["type"])
				assert.Equal(t, "in_process", got.body["status"], "no cambia")
				assert.NotContains(t, outboxTopics(t, w.tenant), "policies.policy_issued")
				return
			}
			assert.Equal(t, "active", got.body["status"])
			details := auditDetails(t, w.tenant, "policies.policy.issued")
			require.Len(t, details, 1)
			assert.Equal(t, c.kycAudit, details[0]["kyc"])
		})
	}
}

func TestKycBlockIsAudited(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world(t)
	id := e.create(t, w).str(t, "id")
	a := e.do(t, w.ops, http.MethodPost, at(id, 1, "accept"), nil)
	*e.flag = flagControl{res: flags.Resolution{Enabled: true, Source: flags.SourceOverride}}
	require.Equal(t, http.StatusConflict, e.do(t, w.ops, http.MethodPost, at(id, a.version(t), "issue"), map[string]any{"policyNumber": "POL-1"}).status)
	details := auditDetails(t, w.tenant, "policies.policy.issue_denied")
	require.Len(t, details, 1)
	assert.Equal(t, "override", details[0]["complianceFlagSource"])
}

func TestRejectAndCancel(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world(t)
	c := e.create(t, w)
	a := e.do(t, w.ops, http.MethodPost, at(c.str(t, "id"), 1, "accept"), nil)
	rej := e.do(t, w.ops, http.MethodPost, at(c.str(t, "id"), a.version(t), "reject"), map[string]any{"reason": "riesgo no asegurable"})
	require.Equal(t, http.StatusOK, rej.status, rej.body)
	assert.Equal(t, "rejected", rej.body["status"])

	p := e.issued(t, w)
	id := p.str(t, "id")
	outside := e.do(t, w.ops, http.MethodPost, at(id, p.version(t), "cancel"), map[string]any{"cause": "client_request", "effectiveDate": "2027-01-01", "unearnedPremium": "0"})
	assert.Equal(t, http.StatusUnprocessableEntity, outside.status)
	assert.Equal(t, "policies.cancellation.outside_coverage_period", outside.body["type"])

	cancel := e.do(t, w.ops, http.MethodPost, at(id, p.version(t), "cancel"), map[string]any{
		"cause": "insurer_revocation", "effectiveDate": "2026-12-31", "unearnedPremium": "500000.25", "insurerReference": "REV-9",
	})
	require.Equal(t, http.StatusOK, cancel.status, cancel.body)
	assert.Equal(t, "cancelled", cancel.body["status"])
	cancellation, ok := cancel.body["cancellation"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "500000.25", cancellation["unearnedPremium"])
	assert.Contains(t, outboxTopics(t, w.tenant), "policies.policy_cancelled")

	assert.Equal(t, http.StatusConflict, e.do(t, w.ops, http.MethodPost, at(id, cancel.version(t), "cancel"), map[string]any{"cause": "other", "effectiveDate": "2026-06-01", "unearnedPremium": "0"}).status, "terminal")
}

func TestEndorsementFlow(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world(t)
	p := e.issued(t, w)
	id := p.str(t, "id")

	outside := e.do(t, w.ops, http.MethodPost, at(id, p.version(t), "endorsements"), map[string]any{"type": "other", "description": "Fuera de vigencia", "effectiveDate": "2027-01-01"})
	assert.Equal(t, "policies.endorsement.outside_coverage_period", outside.body["type"])

	req := e.do(t, w.ops, http.MethodPost, at(id, p.version(t), "endorsements"), map[string]any{"type": "item_change", "description": "Cambio de placa", "effectiveDate": "2026-06-01"})
	require.Equal(t, http.StatusCreated, req.status, req.body)
	assert.Equal(t, "1000000", req.body["netPremium"], "solicitar no cambia la prima")
	assert.NotContains(t, outboxTopics(t, w.tenant), "policies.policy_endorsed")

	endorsements, ok := req.body["endorsements"].([]any)
	require.True(t, ok)
	require.Len(t, endorsements, 1)
	first, ok := endorsements[0].(map[string]any)
	require.True(t, ok)
	endorsementID, ok := first["id"].(string)
	require.True(t, ok)
	path := func(version int, action string) string {
		return fmt.Sprintf("%s/%s/endorsements/%s/%s?version=%d", base, id, endorsementID, action, version)
	}

	coverages, ok := req.body["coverages"].([]any)
	require.True(t, ok)
	coverage, ok := coverages[0].(map[string]any)
	require.True(t, ok)
	items, ok := req.body["items"].([]any)
	require.True(t, ok)
	item, ok := items[0].(map[string]any)
	require.True(t, ok)

	badRisk := e.do(t, w.ops, http.MethodPost, path(req.version(t), "issue"), map[string]any{
		"number": "END-1", "netPremiumDelta": "0", "taxesDelta": "0",
		"itemChanges": []any{map[string]any{"itemId": item["id"], "riskData": map[string]any{"plate": "mal"}}},
	})
	assert.Equal(t, "catalog.risk_data.invalid", badRisk.body["type"], "valida contra el esquema del bien")

	unknownParty := e.do(t, w.ops, http.MethodPost, path(req.version(t), "issue"), map[string]any{
		"number": "END-1", "netPremiumDelta": "0", "taxesDelta": "0",
		"addParties": []any{map[string]any{"partyId": uuid.NewString(), "role": "beneficiary"}},
	})
	assert.Equal(t, "policies.policy.invalid_party", unknownParty.body["type"])

	holderSwap := e.do(t, w.ops, http.MethodPost, path(req.version(t), "issue"), map[string]any{
		"number": "END-1", "netPremiumDelta": "0", "taxesDelta": "0",
		"removeParties": []any{map[string]any{"partyId": w.holder, "role": "holder"}},
		"addParties":    []any{map[string]any{"partyId": w.insured, "role": "holder"}},
	})
	assert.Equal(t, http.StatusUnprocessableEntity, holderSwap.status)
	assert.Equal(t, "policies.endorsement.holder_change_not_allowed", holderSwap.body["type"], "eludiría el control de KYC")

	issued := e.do(t, w.ops, http.MethodPost, path(req.version(t), "issue"), map[string]any{
		"number": "END-1", "netPremiumDelta": "-100000", "taxesDelta": "-19000",
		"coverageChanges": []any{map[string]any{"coverageId": coverage["id"], "sumInsured": "60000000"}},
		"itemChanges":     []any{map[string]any{"itemId": item["id"], "riskData": map[string]any{"plate": "XYZ987", "brand": "Mazda", "model": "3", "year": 2024}}},
		"addParties":      []any{map[string]any{"partyId": w.insured, "role": "beneficiary"}},
	})
	require.Equal(t, http.StatusOK, issued.status, issued.body)
	assert.Equal(t, "900000", issued.body["netPremium"])
	assert.Len(t, issued.body["parties"], 3)
	assert.Contains(t, outboxTopics(t, w.tenant), "policies.policy_endorsed")

	// Guardar otra vez la póliza no borra el historial de efectos del endoso.
	second := e.do(t, w.ops, http.MethodPost, at(id, issued.version(t), "endorsements"), map[string]any{"type": "other", "description": "Otro cambio", "effectiveDate": "2026-07-01"})
	require.Equal(t, http.StatusCreated, second.status, second.body)
	ctx := reqctx.WithTenantID(context.Background(), w.tenant)
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		var effects []byte
		if err := tx.QueryRow(ctx, `SELECT effects FROM policies.endorsements WHERE id = $1`, uuid.MustParse(endorsementID)).Scan(&effects); err != nil {
			return err
		}
		assert.Contains(t, string(effects), "XYZ987")
		return nil
	}))

	already := e.do(t, w.ops, http.MethodPost, path(second.version(t), "reject"), map[string]any{"reason": "ya resuelto"})
	assert.Equal(t, "policies.endorsement.not_pending", already.body["type"])
}

func TestValidationAgainstOtherModules(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world(t)
	mutate := func(k string, v any) map[string]any {
		x := w.terms()
		x[k] = v
		return x
	}
	cases := []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"aseguradora inexistente", mutate("insurerId", uuid.NewString()), http.StatusUnprocessableEntity, "catalog.reference.invalid"},
		{"tercero inexistente", mutate("parties", []any{map[string]any{"partyId": uuid.NewString(), "role": "holder"}, map[string]any{"partyId": w.insured, "role": "insured"}}), http.StatusUnprocessableEntity, "policies.policy.invalid_party"},
		{"sin tomador", mutate("parties", []any{map[string]any{"partyId": w.holder, "role": "insured"}, map[string]any{"partyId": w.insured, "role": "insured"}}), http.StatusUnprocessableEntity, "policies.policy.invalid_holder"},
		{"datos de riesgo inválidos", mutate("items", []any{map[string]any{"description": "Moto", "riskData": map[string]any{"plate": "x"}}}), http.StatusUnprocessableEntity, "catalog.risk_data.invalid"},
		{"datos de salud rechazados", mutate("items", []any{map[string]any{"description": "Vida", "riskData": map[string]any{"plate": "ABC123", "brand": "M", "model": "3", "year": 2024, "health": "x"}}}), http.StatusUnprocessableEntity, "catalog.risk_data.invalid"},
		{"vigencia invertida", mutate("effectiveTo", "2025-01-01"), http.StatusUnprocessableEntity, "kernel.date_range.invalid"},
		{"asesor de otra agencia", mutate("agentId", uuid.NewString()), http.StatusUnprocessableEntity, "policies.policy.invalid_agent"},
		{"prima como número", mutate("netPremium", 1000), http.StatusBadRequest, "platform.bad_request"},
		{"prima negativa", mutate("netPremium", "-1"), http.StatusBadRequest, "platform.bad_request"},
		{"tenant en el cuerpo", mutate("tenantId", uuid.NewString()), http.StatusBadRequest, "platform.bad_request"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := e.do(t, w.ops, http.MethodPost, base, c.body)
			assert.Equal(t, c.status, r.status, r.body)
			assert.Equal(t, c.code, r.body["type"])
		})
	}
}

func TestPermissionsAndOwnership(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world(t)
	agent := e.as(w.tenant, authz.RoleSalesAgent)
	otherAgent := e.as(w.tenant, authz.RoleSalesAgent)
	e.users.list = []identity.User{{ID: uuid.UUID(agent.UserID), DisplayName: "Asesora", Active: true}}

	mine := w.terms()
	mine["agentId"] = uuid.UUID(agent.UserID).String()
	own := e.do(t, w.ops, http.MethodPost, base, mine)
	require.Equal(t, http.StatusCreated, own.status, own.body)
	notMine := e.create(t, w)

	assert.Equal(t, http.StatusOK, e.do(t, agent, http.MethodGet, base+"/"+own.str(t, "id"), nil).status)
	assert.Equal(t, http.StatusNotFound, e.do(t, agent, http.MethodGet, base+"/"+notMine.str(t, "id"), nil).status, "no revela pólizas ajenas")
	assert.Equal(t, http.StatusNotFound, e.do(t, otherAgent, http.MethodGet, base+"/"+own.str(t, "id"), nil).status)

	list := e.do(t, agent, http.MethodGet, base, nil)
	require.Equal(t, http.StatusOK, list.status)
	assert.Len(t, list.body["items"], 1)
	assert.Len(t, e.do(t, w.ops, http.MethodGet, base, nil).body["items"], 2)

	admin := e.as(w.tenant, authz.RoleTenantAdmin)
	id := notMine.str(t, "id")
	cases := []struct {
		name         string
		who          *authz.Principal
		method, path string
		body         any
		want         int
	}{
		{"sin sesión lista", nil, http.MethodGet, base, nil, http.StatusUnauthorized},
		{"sin sesión expide", nil, http.MethodPost, at(id, 1, "issue"), map[string]any{"policyNumber": "X"}, http.StatusUnauthorized},
		{"admin no ve pólizas", admin, http.MethodGet, base, nil, http.StatusForbidden},
		{"admin no ve una póliza", admin, http.MethodGet, base + "/" + id, nil, http.StatusForbidden},
		{"asesor no crea", agent, http.MethodPost, base, w.terms(), http.StatusForbidden},
		{"asesor no expide", agent, http.MethodPost, at(id, 1, "issue"), map[string]any{"policyNumber": "X"}, http.StatusForbidden},
		{"asesor no cancela", agent, http.MethodPost, at(id, 1, "cancel"), map[string]any{"cause": "other", "effectiveDate": "2026-06-01", "unearnedPremium": "0"}, http.StatusForbidden},
		{"asesor no endosa", agent, http.MethodPost, at(id, 1, "endorsements"), map[string]any{"type": "other", "description": "Algo", "effectiveDate": "2026-06-01"}, http.StatusForbidden},
		{"asesor no lista asesores", agent, http.MethodGet, "/api/v1/policies/agents", nil, http.StatusForbidden},
		{"asesor no traspasa", agent, http.MethodPost, "/api/v1/policies/portfolio-transfers", map[string]any{"fromAgentId": uuid.NewString(), "toAgentId": uuid.NewString(), "reason": "Retiro"}, http.StatusForbidden},
		{"gerente lista asesores", e.as(w.tenant, authz.RoleManager), http.MethodGet, "/api/v1/policies/agents", nil, http.StatusOK},
		{"operaciones lista asesores", w.ops, http.MethodGet, "/api/v1/policies/agents", nil, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, e.do(t, c.who, c.method, c.path, c.body).status)
		})
	}
}

func TestTenantIsolation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a, b := e.world(t), e.world(t)
	p := e.issued(t, a)
	id := p.str(t, "id")
	require.Equal(t, http.StatusCreated, e.do(t, a.ops, http.MethodPost, at(id, p.version(t), "endorsements"), map[string]any{"type": "other", "description": "Algo", "effectiveDate": "2026-06-01"}).status)

	assert.Equal(t, http.StatusNotFound, e.do(t, b.ops, http.MethodGet, base+"/"+id, nil).status)
	assert.Equal(t, http.StatusNotFound, e.do(t, b.ops, http.MethodPost, at(id, p.version(t), "cancel"), map[string]any{"cause": "other", "effectiveDate": "2026-06-01", "unearnedPremium": "0"}).status)
	assert.Equal(t, http.StatusNotFound, e.do(t, b.ops, http.MethodPost, at(id, p.version(t), "endorsements"), map[string]any{"type": "other", "description": "Algo", "effectiveDate": "2026-06-01"}).status)
	assert.Empty(t, e.do(t, b.ops, http.MethodGet, base, nil).body["items"])

	foreign := b.terms()
	foreign["parties"] = []any{map[string]any{"partyId": a.holder, "role": "holder"}, map[string]any{"partyId": b.insured, "role": "insured"}}
	assert.Equal(t, "policies.policy.invalid_party", e.do(t, b.ops, http.MethodPost, base, foreign).body["type"], "un tercero de otro tenant no existe")

	countAll := []string{
		`SELECT count(*) FROM policies.policies`, `SELECT count(*) FROM policies.policy_parties`, `SELECT count(*) FROM policies.coverages`,
		`SELECT count(*) FROM policies.insured_items`, `SELECT count(*) FROM policies.endorsements`,
	}
	own := reqctx.WithTenantID(context.Background(), a.tenant)
	require.NoError(t, database.WithTenantTx(own, func(tx pgx.Tx) error {
		for _, q := range countAll {
			var n int
			if err := tx.QueryRow(own, q).Scan(&n); err != nil {
				return err
			}
			assert.Positive(t, n, "A ve sus filas: "+q)
		}
		return nil
	}))
	ctx := reqctx.WithTenantID(context.Background(), b.tenant)
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		for _, q := range countAll {
			var n int
			if err := tx.QueryRow(ctx, q).Scan(&n); err != nil {
				return err
			}
			assert.Zero(t, n, q)
		}
		return nil
	}))
}

// DECISIONS N7: vence el día siguiente al último día cubierto. Cruza tenants y corre como el worker.
func TestExpirationJob(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	due := e.issued(t, w)
	current := e.issued(t, w)
	require.NoError(t, setPeriodEnd(uuid.MustParse(current.str(t, "id")), "2999-12-31"))

	store := postgres.NewStore(workerDB, audit.NewRecorder(e.clock), outbox.NewWriter(e.clock))
	expirer := &app.Expirer{Store: store, Clock: kernel.NewFakeClock(time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC))}
	n, err := expirer.Run(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n, "el último día sigue cubierto")

	expirer.Clock = kernel.NewFakeClock(time.Date(2027, 1, 1, 5, 30, 0, 0, time.UTC)) // 00:30 en Bogotá
	n, err = expirer.Run(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, 1)

	assert.Equal(t, "expired", e.do(t, w.ops, http.MethodGet, base+"/"+due.str(t, "id"), nil).body["status"])
	assert.Equal(t, "active", e.do(t, w.ops, http.MethodGet, base+"/"+current.str(t, "id"), nil).body["status"])
	assert.Contains(t, outboxTopics(t, w.tenant), "policies.policy_expired")
	assert.NotEmpty(t, auditDetails(t, w.tenant, "policies.policy.expired"))

	again, err := expirer.Run(context.Background())
	require.NoError(t, err)
	assert.Zero(t, again, "idempotente")

	// La función que cruza tenants solo es del worker.
	err = database.WithoutTenantTx(context.Background(), func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `SELECT * FROM policies.due_for_expiration('2027-01-01', 10)`)
		return err
	})
	require.ErrorContains(t, err, "permission denied")
}

func setPeriodEnd(id uuid.UUID, end string) error {
	superMu.Lock()
	defer superMu.Unlock()
	_, err := super.Exec(context.Background(), `UPDATE policies.policies SET effective_to = $1::date WHERE id = $2`, end, id)
	return err
}

// Las transiciones que aún no tienen endpoint (las disparan Billing y Renewals en la Fase 3) también
// se guardan y se leen bien; y una póliza en mora se puede endosar y cancelar por la API.
func TestTransitionsWithoutEndpointPersist(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world(t)
	store := postgres.NewStore(database, audit.NewRecorder(e.clock), outbox.NewWriter(e.clock))
	ctx := reqctx.WithTenantID(context.Background(), w.tenant)
	apply := func(id string, fn func(*domain.Policy) error) {
		t.Helper()
		require.NoError(t, store.WithinTenant(ctx, func(ctx context.Context, uow app.UnitOfWork) error {
			p, err := uow.Policies().Get(ctx, domain.PolicyID(uuid.MustParse(id)))
			if err != nil {
				return err
			}
			if err := fn(p); err != nil {
				return err
			}
			return uow.Policies().Save(ctx, p)
		}))
	}
	statusOf := func(id string) any { return e.do(t, w.ops, http.MethodGet, base+"/"+id, nil).body["status"] }
	now := e.clock.Now()

	first := e.issued(t, w).str(t, "id")
	apply(first, func(p *domain.Policy) error { return p.MarkInArrears(now) })
	assert.Equal(t, "in_arrears", statusOf(first))
	apply(first, func(p *domain.Policy) error { return p.Reinstate(now) })
	assert.Equal(t, "active", statusOf(first))
	apply(first, func(p *domain.Policy) error { return p.MarkInArrears(now) })

	cur := e.do(t, w.ops, http.MethodGet, base+"/"+first, nil)
	endorsed := e.do(t, w.ops, http.MethodPost, at(first, cur.version(t), "endorsements"), map[string]any{"type": "other", "description": "En mora", "effectiveDate": "2026-06-01"})
	require.Equal(t, http.StatusCreated, endorsed.status, endorsed.body)
	cancelled := e.do(t, w.ops, http.MethodPost, at(first, endorsed.version(t), "cancel"), map[string]any{"cause": "arrears", "effectiveDate": "2026-06-30", "unearnedPremium": "0"})
	require.Equal(t, http.StatusOK, cancelled.status, cancelled.body)
	assert.Equal(t, "cancelled", cancelled.body["status"])

	end, err := kernel.ParseDate("2027-01-01")
	require.NoError(t, err)
	renewed := e.issued(t, w).str(t, "id")
	replacement := e.issued(t, w).str(t, "id")
	apply(renewed, func(p *domain.Policy) error { return p.Expire(end, now) })
	assert.Equal(t, "expired", statusOf(renewed))
	apply(renewed, func(p *domain.Policy) error {
		return p.MarkRenewed(domain.PolicyID(uuid.MustParse(replacement)), now)
	})
	assert.Equal(t, "renewed", statusOf(renewed))

	lost := e.issued(t, w).str(t, "id")
	apply(lost, func(p *domain.Policy) error { return p.Expire(end, now) })
	apply(lost, func(p *domain.Policy) error { return p.MarkNotRenewed(now) })
	assert.Equal(t, "not_renewed", statusOf(lost))
}

// deliverScopeEvents entrega al consumidor de Parties lo que Policies dejó en el outbox, como el worker.
func deliverScopeEvents(t *testing.T, tenant kernel.TenantID) {
	t.Helper()
	deliver(t, tenant, partiesevents.PolicyScope())
}

// deliver entrega al consumidor lo que hay en el outbox para su tópico. Sin inbox: el consumidor debe
// tolerar repetidos.
func deliver(t *testing.T, tenant kernel.TenantID, consumer outbox.Consumer) {
	t.Helper()
	superMu.Lock()
	rows, err := super.Query(context.Background(), `SELECT id, payload FROM messaging.outbox WHERE tenant_id = $1 AND topic = $2 ORDER BY id`, uuid.UUID(tenant), consumer.Topic)
	require.NoError(t, err)
	var events []outbox.Event
	for rows.Next() {
		var e outbox.Event
		require.NoError(t, rows.Scan(&e.ID, &e.Payload))
		e.TenantID, e.OccurredAt = tenant, time.Now().UTC() //nolint:forbidigo // instante informativo de la proyección
		events = append(events, e)
	}
	rows.Close()
	superMu.Unlock()
	ctx := reqctx.WithTenantID(context.Background(), tenant)
	for _, e := range events {
		require.NoError(t, workerDB.WithTenantTx(ctx, func(tx pgx.Tx) error { return consumer.Handle(ctx, tx, e) }))
	}
}

// DECISIONS N10: su cliente es quien registró o una parte de sus pólizas.
func TestAgentOnlyReachesOwnClients(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world(t)
	agent := e.as(w.tenant, authz.RoleSalesAgent)
	other := e.as(w.tenant, authz.RoleSalesAgent)
	e.users.list = []identity.User{{ID: uuid.UUID(agent.UserID), DisplayName: "Asesora", Active: true}}
	const parties = "/api/v1/parties/parties"
	client := func(who *authz.Principal, id string) int {
		return e.do(t, who, http.MethodGet, parties+"/"+id, nil).status
	}
	listed := func(who *authz.Principal) int {
		items, ok := e.do(t, who, http.MethodGet, parties, nil).body["items"].([]any)
		require.True(t, ok)
		return len(items)
	}

	// Sin póliza: los clientes que registró Operaciones no son suyos.
	assert.Equal(t, http.StatusNotFound, client(agent, w.holder))
	assert.Zero(t, listed(agent))

	// Registra uno: lo ve y lo edita; el otro asesor no.
	number := fmt.Sprintf("1%09d", uuid.New().ID()%1_000_000_000)
	mine := e.do(t, agent, http.MethodPost, parties, map[string]any{"kind": "person", "name": "Cliente propio", "documentType": "CC", "documentNumber": number})
	require.Equal(t, http.StatusCreated, mine.status, mine.body)
	mineID := mine.str(t, "id")
	assert.Equal(t, http.StatusOK, client(agent, mineID))
	assert.Equal(t, 1, listed(agent))
	assert.Equal(t, http.StatusNotFound, client(other, mineID))
	assert.Equal(t, http.StatusOK, e.do(t, agent, http.MethodPut, parties+"/"+mineID, map[string]any{"name": "Cliente editado", "version": 1}).status)
	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodPut, parties+"/"+mineID, map[string]any{"name": "Robado", "version": 2}).status)
	consent := map[string]any{"status": "granted", "purpose": "Gestión", "textVersion": "v1", "consentDate": kernel.Today(kernel.SystemClock{}).String()}
	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodGet, parties+"/"+mineID+"/consents", nil).status)
	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodPost, parties+"/"+mineID+"/consents", consent).status)

	// No corrige el documento, ni siquiera de sus clientes.
	rectify := map[string]any{"name": "Cliente editado", "version": 2, "document": map[string]any{"documentType": "CC", "documentNumber": "12345678"}}
	assert.Equal(t, http.StatusForbidden, e.do(t, agent, http.MethodPut, parties+"/"+mineID, rectify).status)

	// El documento de otro asesor: aviso sin datos, búsqueda sin resultado, y ambos auditados.
	dup := e.do(t, other, http.MethodPost, parties, map[string]any{"kind": "person", "name": "Duplicado", "documentType": "CC", "documentNumber": number})
	assert.Equal(t, http.StatusConflict, dup.status)
	assert.Equal(t, "parties.party.document_taken", dup.body["type"])
	assert.NotContains(t, fmt.Sprint(dup.body), "Cliente")
	assert.Len(t, auditDetails(t, w.tenant, "parties.party.duplicate_rejected"), 1)
	ctx := reqctx.WithTenantID(context.Background(), w.tenant)
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		var probed uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT resource_id::uuid FROM audit.events WHERE action = 'parties.party.duplicate_rejected'`).Scan(&probed); err != nil {
			return err
		}
		assert.Equal(t, mineID, probed.String(), "identifica al cliente sondeado")
		return nil
	}))
	search := map[string]any{"documentType": "CC", "documentNumber": number}
	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodPost, parties+"/search-by-document", search).status)
	assert.Equal(t, http.StatusOK, e.do(t, agent, http.MethodPost, parties+"/search-by-document", search).status)
	searches := auditDetails(t, w.tenant, "parties.party.document_searched")
	require.Len(t, searches, 2)
	assert.Equal(t, "true", searches[0]["outOfScope"])
	assert.Equal(t, "false", searches[1]["outOfScope"])

	// Asignado como asesor de una póliza, alcanza a sus partes cuando llega el evento.
	terms := w.terms()
	terms["agentId"] = uuid.UUID(agent.UserID).String()
	policy := e.do(t, w.ops, http.MethodPost, base, terms)
	require.Equal(t, http.StatusCreated, policy.status, policy.body)
	assert.Equal(t, http.StatusNotFound, client(agent, w.holder), "antes de procesar el evento")
	deliverScopeEvents(t, w.tenant)
	assert.Equal(t, http.StatusOK, client(agent, w.holder))
	assert.Equal(t, http.StatusOK, client(agent, w.insured))
	assert.Equal(t, 3, listed(agent))
	assert.Equal(t, http.StatusNotFound, client(other, w.holder))

	// Una entrega repetida no retrocede; si le quitan la póliza, pierde el alcance.
	deliverScopeEvents(t, w.tenant)
	assert.Equal(t, http.StatusOK, client(agent, w.holder))
	terms["agentId"] = nil
	delete(terms, "agentId")
	updated := e.do(t, w.ops, http.MethodPut, fmt.Sprintf("%s/%s?version=%d", base, policy.str(t, "id"), policy.version(t)), terms)
	require.Equal(t, http.StatusOK, updated.status, updated.body)
	deliverScopeEvents(t, w.tenant)
	assert.Equal(t, http.StatusNotFound, client(agent, w.holder))
	assert.Equal(t, http.StatusOK, client(agent, mineID), "conserva aquello de lo que es responsable")

	// Operaciones sigue alcanzando a todos y puede corregir el documento.
	rectify["version"] = 2
	assert.Equal(t, http.StatusOK, e.do(t, w.ops, http.MethodPut, parties+"/"+mineID, rectify).status)
}

// El consumidor descarta versiones viejas aunque lleguen después de la nueva.
func TestPolicyScopeIgnoresOutOfOrderEvents(t *testing.T) {
	t.Parallel()
	tenant, policyID, agent, party := newTenant(), uuid.New(), uuid.New(), uuid.New()
	ctx := reqctx.WithTenantID(context.Background(), tenant)
	consumer := partiesevents.PolicyScope()
	deliver := func(version int32, agentID *uuid.UUID) {
		payload, err := json.Marshal(policycontracts.PolicyPartiesChanged{PolicyID: policyID, PolicyVersion: version, AgentID: agentID, PartyIDs: []uuid.UUID{party}, GrantsAccess: true})
		require.NoError(t, err)
		event := outbox.Event{ID: uuid.New(), TenantID: tenant, OccurredAt: time.Now().UTC(), Payload: payload} //nolint:forbidigo // instante informativo
		require.NoError(t, workerDB.WithTenantTx(ctx, func(tx pgx.Tx) error { return consumer.Handle(ctx, tx, event) }))
	}
	deliver(3, nil)
	deliver(2, &agent)
	var stored uuid.NullUUID
	var version int32
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT agent_id, policy_version FROM parties.policy_scopes WHERE policy_id = $1`, policyID).Scan(&stored, &version)
	}))
	assert.False(t, stored.Valid, "la versión 2 llegó tarde: se descarta")
	assert.Equal(t, int32(3), version)

	// Solo el worker escribe el alcance: la API no puede forjarlo.
	err := database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE parties.policy_scopes SET agent_id = $1 WHERE policy_id = $2`, agent, policyID)
		return err
	})
	require.ErrorContains(t, err, "permission denied")
}

// DECISIONS N11: el acceso sigue a la asignación vigente.
func TestPortfolioFollowsTheCurrentAssignment(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world(t)
	manager := e.as(w.tenant, authz.RoleManager)
	ana, beto := e.as(w.tenant, authz.RoleSalesAgent), e.as(w.tenant, authz.RoleSalesAgent)
	gone := e.as(w.tenant, authz.RoleSalesAgent)
	e.users.list = []identity.User{
		{ID: uuid.UUID(ana.UserID), DisplayName: "Ana", Active: true},
		{ID: uuid.UUID(beto.UserID), DisplayName: "Beto", Active: true},
		{ID: uuid.UUID(gone.UserID), DisplayName: "Se fue"},
	}
	const parties = "/api/v1/parties/parties"
	client := func(who *authz.Principal, id string) int {
		return e.do(t, who, http.MethodGet, parties+"/"+id, nil).status
	}
	agentID := func(p *authz.Principal) string { return uuid.UUID(p.UserID).String() }

	// Traspaso de un cliente: el anterior pierde el acceso; quien lo registró no lo conserva.
	number := fmt.Sprintf("1%09d", uuid.New().ID()%1_000_000_000)
	created := e.do(t, ana, http.MethodPost, parties, map[string]any{"kind": "person", "name": "Cliente de Ana", "documentType": "CC", "documentNumber": number})
	require.Equal(t, http.StatusCreated, created.status, created.body)
	clientID := created.str(t, "id")
	assert.Equal(t, agentID(ana), created.body["responsibleAgentId"], "quien registra queda como responsable")
	byOps := e.do(t, w.ops, http.MethodGet, parties+"/"+w.holder, nil)
	assert.Nil(t, byOps.body["responsibleAgentId"], "lo que registra Operaciones no tiene responsable")

	assign := func(who *authz.Principal, agent string, version int) resp {
		return e.do(t, who, http.MethodPost, parties+"/"+clientID+"/responsible-agent", map[string]any{"agentId": agent, "reason": "Retiro del asesor", "version": version})
	}
	assert.Equal(t, http.StatusForbidden, assign(ana, agentID(beto), 1).status, "un asesor no traspasa")
	assert.Equal(t, "parties.party.invalid_agent", assign(manager, agentID(gone), 1).body["type"], "no se asigna a quien ya se fue")
	assert.Equal(t, http.StatusConflict, assign(manager, agentID(beto), 9).status)
	moved := assign(manager, agentID(beto), 1)
	require.Equal(t, http.StatusOK, moved.status, moved.body)
	assert.Equal(t, http.StatusNotFound, client(ana, clientID))
	assert.Equal(t, http.StatusOK, client(beto, clientID))
	assert.Len(t, auditDetails(t, w.tenant, "parties.party.responsible_changed"), 1)

	// Cambio de asesor de una póliza: historial y acceso.
	terms := w.terms()
	terms["agentId"] = agentID(ana)
	created = e.do(t, w.ops, http.MethodPost, base, terms)
	require.Equal(t, http.StatusCreated, created.status, created.body)
	policyID := created.str(t, "id")
	deliverScopeEvents(t, w.tenant)
	assert.Equal(t, http.StatusOK, client(ana, w.holder))

	reassign := func(who *authz.Principal, agent string, version int) resp {
		return e.do(t, who, http.MethodPost, at(policyID, version, "agent"), map[string]any{"agentId": agent, "reason": "Retiro del asesor"})
	}
	assert.Equal(t, http.StatusForbidden, reassign(ana, agentID(beto), created.version(t)).status)
	assert.Equal(t, "policies.policy.invalid_agent", reassign(manager, agentID(gone), created.version(t)).body["type"])
	changed := reassign(manager, agentID(beto), created.version(t))
	require.Equal(t, http.StatusOK, changed.status, changed.body)
	deliverScopeEvents(t, w.tenant)
	assert.Equal(t, http.StatusNotFound, client(ana, w.holder))
	assert.Equal(t, http.StatusOK, client(beto, w.holder))
	assert.Equal(t, http.StatusNotFound, e.do(t, ana, http.MethodGet, base+"/"+policyID, nil).status)
	assert.Equal(t, http.StatusOK, e.do(t, beto, http.MethodGet, base+"/"+policyID, nil).status)
	assert.Contains(t, outboxTopics(t, w.tenant), "policies.policy_agent_changed")
	ctx := reqctx.WithTenantID(context.Background(), w.tenant)
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		var n int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM policies.policy_agent_history WHERE policy_id = $1`, uuid.MustParse(policyID)).Scan(&n)
		assert.Equal(t, 2, n, "asignación inicial y traspaso")
		return err
	}))
	err := database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM policies.policy_agent_history`)
		return err
	})
	require.ErrorContains(t, err, "permission denied", "append-only")

	// Una póliza terminada deja de dar acceso, pero el asesor sigue viéndola como historial.
	a := e.do(t, w.ops, http.MethodPost, at(policyID, changed.version(t), "accept"), nil)
	require.Equal(t, http.StatusOK, a.status, a.body)
	rejected := e.do(t, w.ops, http.MethodPost, at(policyID, a.version(t), "reject"), map[string]any{"reason": "Riesgo no asegurable"})
	require.Equal(t, http.StatusOK, rejected.status, rejected.body)
	deliverScopeEvents(t, w.tenant)
	assert.Equal(t, http.StatusNotFound, client(beto, w.holder))
	assert.Equal(t, http.StatusOK, e.do(t, beto, http.MethodGet, base+"/"+policyID, nil).status)
	assert.Equal(t, "policies.policy.agent_not_reassignable", reassign(manager, agentID(ana), rejected.version(t)).body["type"])
}

func TestPortfolioTransfer(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world(t)
	manager := e.as(w.tenant, authz.RoleManager)
	from, to := e.as(w.tenant, authz.RoleSalesAgent), e.as(w.tenant, authz.RoleSalesAgent)
	e.users.list = []identity.User{
		{ID: uuid.UUID(from.UserID), DisplayName: "Se va", Active: true},
		{ID: uuid.UUID(to.UserID), DisplayName: "Nueva", Active: true},
	}
	const parties = "/api/v1/parties/parties"
	fromID, toID := uuid.UUID(from.UserID).String(), uuid.UUID(to.UserID).String()

	// Cartera del que se va: un cliente, una póliza vigente y una terminada.
	number := fmt.Sprintf("1%09d", uuid.New().ID()%1_000_000_000)
	own := e.do(t, from, http.MethodPost, parties, map[string]any{"kind": "person", "name": "Cliente", "documentType": "CC", "documentNumber": number})
	require.Equal(t, http.StatusCreated, own.status, own.body)
	terms := w.terms()
	terms["agentId"] = fromID
	active := e.do(t, w.ops, http.MethodPost, base, terms)
	require.Equal(t, http.StatusCreated, active.status, active.body)
	finished := e.do(t, w.ops, http.MethodPost, base, terms)
	require.Equal(t, http.StatusCreated, finished.status, finished.body)
	acc := e.do(t, w.ops, http.MethodPost, at(finished.str(t, "id"), 1, "accept"), nil)
	require.Equal(t, http.StatusOK, e.do(t, w.ops, http.MethodPost, at(finished.str(t, "id"), acc.version(t), "reject"), map[string]any{"reason": "Rechazada"}).status)
	e.users.list[0].Active = false // se retira

	transfer := func(who *authz.Principal, a, b string) resp {
		return e.do(t, who, http.MethodPost, "/api/v1/policies/portfolio-transfers", map[string]any{"fromAgentId": a, "toAgentId": b, "reason": "Retiro del asesor"})
	}
	assert.Equal(t, "policies.policy.same_agent", transfer(manager, toID, toID).body["type"])
	assert.Equal(t, "policies.policy.invalid_agent", transfer(manager, toID, fromID).body["type"], "el destino debe estar activo")
	assert.Equal(t, "policies.policy.invalid_agent", transfer(manager, uuid.NewString(), toID).body["type"], "el origen debe ser de la agencia")
	require.Equal(t, http.StatusAccepted, transfer(manager, fromID, toID).status, "el origen puede ser quien ya se fue")
	assert.Len(t, auditDetails(t, w.tenant, "policies.portfolio.transfer_requested"), 1)

	recorder := audit.NewRecorder(e.clock)
	store := postgres.NewStore(workerDB, recorder, outbox.NewWriter(e.clock))
	deliver(t, w.tenant, policiesevents.PortfolioTransfer(store, e.clock))
	deliver(t, w.tenant, partiesevents.PortfolioTransfer(recorder))
	deliverScopeEvents(t, w.tenant)

	assert.Equal(t, toID, e.do(t, w.ops, http.MethodGet, base+"/"+active.str(t, "id"), nil).body["agentId"])
	assert.Equal(t, fromID, e.do(t, w.ops, http.MethodGet, base+"/"+finished.str(t, "id"), nil).body["agentId"], "la terminada conserva su asesor como historial")
	assert.Equal(t, http.StatusOK, e.do(t, to, http.MethodGet, parties+"/"+own.str(t, "id"), nil).status)
	assert.Equal(t, http.StatusOK, e.do(t, to, http.MethodGet, parties+"/"+w.holder, nil).status)
	assert.Equal(t, http.StatusNotFound, e.do(t, from, http.MethodGet, parties+"/"+own.str(t, "id"), nil).status)
	assert.Equal(t, http.StatusNotFound, e.do(t, from, http.MethodGet, parties+"/"+w.holder, nil).status)
	clients := auditDetails(t, w.tenant, "parties.portfolio.transferred")
	require.Len(t, clients, 1)
	assert.Equal(t, "1", clients[0]["clients"])
}
