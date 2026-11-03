//go:build integration

package cataloghttp_test

import (
	"bytes"
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

	"github.com/marvinrepremarca/seguros/internal/modules/catalog"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/pgtest"
)

var database *db.DB //nolint:gochecknoglobals // compartido por la suite (TestMain)

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
	database, err = env.OpenApp(ctx)
	if err != nil {
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer database.Close()
	return m.Run()
}

type principalKey struct{}

type env struct {
	router *chi.Mux
	mod    *catalog.Module
	policy *authz.Policy
}

func newEnv(t *testing.T) *env {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.NewFakeClock(time.Now().UTC()) //nolint:forbidigo // partición de auditoría del mes actual
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	mod, err := catalog.New(catalog.Deps{DB: database, Audit: audit.NewRecorder(clock), Policy: policy, Clock: clock, Logger: logger})
	require.NoError(t, err)
	root := httpx.NewRouter(httpx.RouterOptions{Logger: logger, Clock: clock, MaxBodyBytes: 1 << 20})
	var mountErr error
	root.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := req.Context()
				if p, ok := ctx.Value(principalKey{}).(authz.Principal); ok {
					ctx = authz.WithPrincipal(ctx, p)
				}
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		mountErr = mod.Mount(r)
	})
	require.NoError(t, mountErr)
	return &env{router: root, mod: mod, policy: policy}
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
	out := resp{status: rec.Code, body: map[string]any{}}
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out.body), rec.Body.String())
	}
	return out
}

func (e *env) created(t *testing.T, who *authz.Principal, path string, body any) string {
	t.Helper()
	r := e.do(t, who, http.MethodPost, path, body)
	require.Equal(t, http.StatusCreated, r.status, r.body)
	id, ok := r.body["id"].(string)
	require.True(t, ok)
	return id
}

const (
	insurers   = "/api/v1/catalog/insurers"
	lines      = "/api/v1/catalog/lines-of-business"
	products   = "/api/v1/catalog/products"
	agreements = "/api/v1/catalog/commission-agreements"
	schemas    = "/api/v1/catalog/risk-schemas"
)

func TestItemsLifecycleAndUniqueNames(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	admin := e.as(newTenant(), authz.RoleTenantAdmin)

	id := e.created(t, admin, insurers, map[string]any{"name": "Sura"})
	dup := e.do(t, admin, http.MethodPost, insurers, map[string]any{"name": "SURA"})
	assert.Equal(t, http.StatusConflict, dup.status)
	assert.Equal(t, "catalog.item.name_taken", dup.body["type"])
	assert.Equal(t, http.StatusCreated, e.do(t, e.as(newTenant(), authz.RoleTenantAdmin), http.MethodPost, insurers, map[string]any{"name": "Sura"}).status, "otro tenant")

	updated := e.do(t, admin, http.MethodPut, insurers+"/"+id, map[string]any{"name": "Sura S.A.", "active": false, "version": 1})
	require.Equal(t, http.StatusOK, updated.status, updated.body)
	assert.InDelta(t, 2, updated.body["version"], 0)
	assert.Equal(t, http.StatusConflict, e.do(t, admin, http.MethodPut, insurers+"/"+id, map[string]any{"name": "Otra", "active": true, "version": 1}).status)
	assert.Equal(t, http.StatusNotFound, e.do(t, admin, http.MethodPut, insurers+"/"+uuid.NewString(), map[string]any{"name": "Otra", "active": true, "version": 1}).status)

	lineID := e.created(t, admin, lines, map[string]any{"name": "Autos", "riskSchemaKey": "auto"})
	assert.Equal(t, http.StatusBadRequest, e.do(t, admin, http.MethodPost, lines, map[string]any{"name": "Naves", "riskSchemaKey": "nave"}).status)
	line := e.do(t, admin, http.MethodPut, lines+"/"+lineID, map[string]any{"name": "Automóviles", "active": true, "version": 1})
	assert.Equal(t, "auto", line.body["riskSchemaKey"], "el esquema no cambia")

	// Aseguradora inactiva: no admite productos nuevos.
	inactive := e.do(t, admin, http.MethodPost, products, map[string]any{"name": "Plan", "insurerId": id, "lineOfBusinessId": lineID})
	assert.Equal(t, http.StatusUnprocessableEntity, inactive.status)
	assert.Equal(t, "catalog.reference.invalid", inactive.body["type"])
}

func TestListPaginationAndSchemas(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	admin := e.as(newTenant(), authz.RoleTenantAdmin)
	for _, n := range []string{"Sura", "Bolívar", "Allianz"} {
		e.created(t, admin, insurers, map[string]any{"name": n})
	}
	first := e.do(t, admin, http.MethodGet, insurers+"?limit=2", nil)
	require.Equal(t, http.StatusOK, first.status)
	assert.Len(t, first.body["items"], 2)
	cursor, ok := first.body["nextCursor"].(string)
	require.True(t, ok)
	second := e.do(t, admin, http.MethodGet, insurers+"?limit=2&cursor="+cursor, nil)
	assert.Len(t, second.body["items"], 1)

	list := e.do(t, admin, http.MethodGet, schemas, nil)
	require.Equal(t, http.StatusOK, list.status)
	assert.Len(t, list.body["items"], 5)
}

func TestAgreementsCannotOverlap(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tenant := newTenant()
	admin := e.as(tenant, authz.RoleTenantAdmin)
	manager := e.as(tenant, authz.RoleManager)
	insurer := e.created(t, admin, insurers, map[string]any{"name": "Sura"})
	other := e.created(t, admin, insurers, map[string]any{"name": "Bolívar"})
	line := e.created(t, admin, lines, map[string]any{"name": "Autos", "riskSchemaKey": "auto"})
	product := e.created(t, admin, products, map[string]any{"name": "Plan Oro", "insurerId": insurer, "lineOfBusinessId": line})

	base := map[string]any{"insurerId": insurer, "lineOfBusinessId": line, "percentage": "12.5", "validFrom": "2026-01-01", "validTo": "2026-12-31"}
	created := e.do(t, manager, http.MethodPost, agreements, base)
	require.Equal(t, http.StatusCreated, created.status, created.body)
	assert.Equal(t, "paid_premium", created.body["basis"], "N3: base por defecto")
	assert.Equal(t, "12.5", created.body["percentage"], "string decimal en el wire")
	id, ok := created.body["id"].(string)
	require.True(t, ok)

	overlap := map[string]any{"insurerId": insurer, "lineOfBusinessId": line, "percentage": "10", "validFrom": "2026-12-31", "validTo": "2027-12-31"}
	r := e.do(t, manager, http.MethodPost, agreements, overlap)
	assert.Equal(t, http.StatusConflict, r.status, "la fecha final es inclusiva")
	assert.Equal(t, "catalog.commission_agreement.overlap", r.body["type"])

	overlap["validFrom"] = "2027-01-01"
	assert.Equal(t, http.StatusCreated, e.do(t, manager, http.MethodPost, agreements, overlap).status, "vigencia contigua")

	withProduct := map[string]any{"insurerId": insurer, "lineOfBusinessId": line, "productId": product, "percentage": "15", "validFrom": "2026-01-01", "validTo": "2026-12-31"}
	assert.Equal(t, http.StatusCreated, e.do(t, manager, http.MethodPost, agreements, withProduct).status, "otro ámbito: por producto")

	wrong := map[string]any{"insurerId": other, "lineOfBusinessId": line, "productId": product, "percentage": "15", "validFrom": "2026-01-01", "validTo": "2026-12-31"}
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, manager, http.MethodPost, agreements, wrong).status, "producto de otra aseguradora")

	assert.Equal(t, http.StatusBadRequest, e.do(t, manager, http.MethodPost, agreements, map[string]any{"insurerId": insurer, "lineOfBusinessId": line, "percentage": 12.5, "validFrom": "2026-01-01", "validTo": "2026-12-31"}).status, "un número en el wire se rechaza")
	bad := e.do(t, manager, http.MethodPut, agreements+"/"+id, map[string]any{"basis": "paid_premium", "percentage": "101", "validFrom": "2026-01-01", "validTo": "2026-12-31", "version": 1})
	assert.Equal(t, "catalog.commission_agreement.invalid_percentage", bad.body["type"])

	updated := e.do(t, manager, http.MethodPut, agreements+"/"+id, map[string]any{"basis": "written_premium", "percentage": "11", "validFrom": "2026-01-01", "validTo": "2026-12-31", "version": 1})
	require.Equal(t, http.StatusOK, updated.status, updated.body)
	assert.Equal(t, "written_premium", updated.body["basis"])
}

func TestPermissions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tenant := newTenant()
	sales := e.as(tenant, authz.RoleSalesAgent)
	accounting := e.as(tenant, authz.RoleAccounting)
	cases := []struct {
		name         string
		who          *authz.Principal
		method, path string
		body         any
		want         int
	}{
		{"sin sesión", nil, http.MethodGet, insurers, nil, http.StatusUnauthorized},
		{"asesor lee", sales, http.MethodGet, insurers, nil, http.StatusOK},
		{"asesor no crea", sales, http.MethodPost, insurers, map[string]any{"name": "Sura"}, http.StatusForbidden},
		{"asesor no ve comisiones", sales, http.MethodGet, agreements, nil, http.StatusForbidden},
		{"contabilidad ve comisiones", accounting, http.MethodGet, agreements, nil, http.StatusOK},
		{"contabilidad no las cambia", accounting, http.MethodPost, agreements, map[string]any{"insurerId": uuid.NewString(), "lineOfBusinessId": uuid.NewString(), "percentage": "1", "validFrom": "2026-01-01", "validTo": "2026-02-01"}, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, e.do(t, c.who, c.method, c.path, c.body).status)
		})
	}
}

func TestTenantIsolationAndDirectory(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ta, tb := newTenant(), newTenant()
	a, b := e.as(ta, authz.RoleTenantAdmin), e.as(tb, authz.RoleTenantAdmin)
	insurer := e.created(t, a, insurers, map[string]any{"name": "Sura"})
	line := e.created(t, a, lines, map[string]any{"name": "Autos", "riskSchemaKey": "auto"})
	product := e.created(t, a, products, map[string]any{"name": "Plan", "insurerId": insurer, "lineOfBusinessId": line})

	assert.Empty(t, e.do(t, b, http.MethodGet, insurers, nil).body["items"])
	assert.Equal(t, http.StatusNotFound, e.do(t, b, http.MethodPut, insurers+"/"+insurer, map[string]any{"name": "Robada", "active": true, "version": 1}).status)
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, b, http.MethodPost, products, map[string]any{"name": "Plan", "insurerId": insurer, "lineOfBusinessId": line}).status)

	ctxA := reqctx.WithTenantID(context.Background(), ta)
	ctxB := reqctx.WithTenantID(context.Background(), tb)
	ref := contracts.PolicyRef{
		InsurerID: uuid.MustParse(insurer), LineOfBusinessID: uuid.MustParse(line),
		ProductID: uuid.NullUUID{UUID: uuid.MustParse(product), Valid: true},
	}
	key, err := e.mod.Directory.ResolvePolicyRef(ctxA, ref)
	require.NoError(t, err)
	assert.Equal(t, "auto", key)
	_, err = e.mod.Directory.ResolvePolicyRef(ctxB, ref)
	require.ErrorIs(t, err, contracts.ErrInvalidReference, "otro tenant no resuelve")

	require.NoError(t, database.WithTenantTx(ctxB, func(tx pgx.Tx) error {
		for _, q := range []string{
			`SELECT count(*) FROM catalog.insurers`, `SELECT count(*) FROM catalog.lines_of_business`,
			`SELECT count(*) FROM catalog.products`, `SELECT count(*) FROM catalog.commission_agreements`,
		} {
			var n int
			if err := tx.QueryRow(ctxB, q).Scan(&n); err != nil {
				return err
			}
			assert.Zero(t, n, q)
		}
		return nil
	}))
}
