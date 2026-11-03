//go:build integration

package featuremgmthttp_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt"
	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/app"
	tenancy "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
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

type directory struct {
	mu    sync.Mutex
	plans map[kernel.TenantID]string
}

func (d *directory) ResolveByOrgAlias(context.Context, string) (tenancy.TenantInfo, error) {
	return tenancy.TenantInfo{}, tenancy.ErrTenantNotFound
}

func (d *directory) Get(_ context.Context, id kernel.TenantID) (tenancy.TenantInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.plans[id]
	if !ok {
		return tenancy.TenantInfo{}, tenancy.ErrTenantNotFound
	}
	return tenancy.TenantInfo{ID: id, Plan: p, Active: true}, nil
}

type env struct {
	router *chi.Mux
	mod    *featuremgmt.Module
	dir    *directory
	policy *authz.Policy
	eval   *flags.Evaluator
}

// principalKey lleva el principal de prueba en la petición (sustituye a la sesión del BFF).
type principalKey struct{}

func newEnv(t *testing.T) *env {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.NewFakeClock(time.Now().UTC()) //nolint:forbidigo // partición de auditoría del mes actual
	reg, err := flags.LoadRegistry()
	require.NoError(t, err)
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	dir := &directory{plans: map[kernel.TenantID]string{}}
	store := featuremgmt.NewStore(database, audit.NewRecorder(clock), outbox.NewWriter(clock))
	eval := flags.NewEvaluator(reg, store, nil, clock, logger)
	mod := featuremgmt.New(featuremgmt.Deps{
		Store: store, Tenants: dir, Registry: reg, Invalidator: flags.NewInvalidator(eval, nil),
		Checker: eval, Policy: policy, Clock: clock, Logger: logger,
	})
	root := httpx.NewRouter(httpx.RouterOptions{Logger: logger, Clock: clock, MaxBodyBytes: 1 << 20})
	var mountErr error
	root.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := req.Context()
				if p, ok := ctx.Value(principalKey{}).(authz.Principal); ok {
					ctx = authz.WithPrincipal(ctx, p)
					info, err := dir.Get(ctx, p.TenantID)
					require.NoError(t, err)
					ctx = reqctx.WithTenantPlan(ctx, info.Plan)
				}
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		mountErr = mod.Mount(r)
		// Ruta de un módulo apagable de ejemplo protegida por su flag (las 4 capas: aquí el endpoint).
		r.With(flags.Require(eval, flags.ClaimsModule)).Get("/api/v1/claims/claims", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})
	require.NoError(t, mountErr)
	return &env{router: root, mod: mod, dir: dir, policy: policy, eval: eval}
}

func (e *env) tenant(plan string) kernel.TenantID {
	id := kernel.TenantID(uuid.Must(uuid.NewV7()))
	e.dir.mu.Lock()
	e.dir.plans[id] = plan
	e.dir.mu.Unlock()
	return id
}

func (e *env) call(t *testing.T, tenant kernel.TenantID, role authz.Role, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := t.Context()
	if role != "" {
		p := authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), tenant, []authz.Role{role}, e.policy)
		ctx = context.WithValue(ctx, principalKey{}, p)
	}
	req := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *env) platformSet(t *testing.T, tenant kernel.TenantID, key flags.Key, enabled *bool) {
	t.Helper()
	_, err := e.mod.PlatformSetFlag.Handle(reqctx.WithTenantID(context.Background(), tenant),
		app.SetTenantFlagCommand{Key: key, Enabled: enabled, Reason: "add-on contratado"})
	require.NoError(t, err)
}

func boolPtr(v bool) *bool { return &v }

func TestFlaggedEndpointOnOffAndTenantIsolation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a, b := e.tenant("basico"), e.tenant("basico") // claims no está en el plan básico

	assert.Equal(t, http.StatusNotFound, e.call(t, a, authz.RoleSalesAgent, http.MethodGet, "/api/v1/claims/claims", "").Code, "OFF → 404")
	e.platformSet(t, a, flags.ClaimsModule, boolPtr(true))
	assert.Equal(t, http.StatusOK, e.call(t, a, authz.RoleSalesAgent, http.MethodGet, "/api/v1/claims/claims", "").Code, "ON (add-on) → 200")
	assert.Equal(t, http.StatusNotFound, e.call(t, b, authz.RoleSalesAgent, http.MethodGet, "/api/v1/claims/claims", "").Code, "B sigue apagado")

	e.platformSet(t, a, flags.ClaimsModule, nil)
	assert.Equal(t, http.StatusNotFound, e.call(t, a, authz.RoleSalesAgent, http.MethodGet, "/api/v1/claims/claims", "").Code, "clear → vuelve al plan")
}

func TestGetFeatures(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a := e.tenant("profesional")
	rec := e.call(t, a, authz.RoleSalesAgent, http.MethodGet, "/bff/features", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var out struct {
		Flags map[string]bool `json:"flags"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	assert.True(t, out.Flags[string(flags.ClaimsModule)], "profesional incluye siniestros")
	assert.False(t, out.Flags[string(flags.CommissionsModule)], "comisiones es empresarial")
	assert.Len(t, out.Flags, 11)

	assert.Equal(t, http.StatusUnauthorized, e.call(t, a, "", http.MethodGet, "/bff/features", "").Code)
}

func TestListAndSetFlagsEndpoints(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a := e.tenant("empresarial")
	e.platformSet(t, a, flags.ReportingModule, boolPtr(false))

	rec := e.call(t, a, authz.RoleManager, http.MethodGet, "/api/v1/featuremgmt/flags", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var list struct {
		Items []struct {
			Key      string `json:"key"`
			Enabled  bool   `json:"enabled"`
			Source   string `json:"source"`
			Override *struct {
				Reason string `json:"reason"`
			} `json:"override"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&list))
	require.Len(t, list.Items, 11)
	for _, it := range list.Items {
		if it.Key == string(flags.ReportingModule) {
			assert.False(t, it.Enabled)
			assert.Equal(t, "override", it.Source)
			require.NotNil(t, it.Override)
			assert.Equal(t, "add-on contratado", it.Override.Reason)
		}
	}

	assert.Equal(t, http.StatusForbidden, e.call(t, a, authz.RoleSalesAgent, http.MethodGet, "/api/v1/featuremgmt/flags", "").Code)
	assert.Equal(t, http.StatusUnauthorized, e.call(t, a, "", http.MethodGet, "/api/v1/featuremgmt/flags", "").Code)

	body := `{"action":"enable","reason":"probar"}`
	assert.Equal(t, http.StatusForbidden, e.call(t, a, authz.RoleManager, http.MethodPut, "/api/v1/featuremgmt/flags/module.claims", body).Code, "Manager no actualiza")
	rec = e.call(t, a, authz.RoleTenantAdmin, http.MethodPut, "/api/v1/featuremgmt/flags/module.claims", body)
	assert.Equal(t, http.StatusForbidden, rec.Code, "los módulos no son editables por el tenant")
	assert.Contains(t, rec.Body.String(), "featuremgmt.flag.not_tenant_editable")
	assert.Equal(t, http.StatusNotFound, e.call(t, a, authz.RoleTenantAdmin, http.MethodPut, "/api/v1/featuremgmt/flags/module.nada", body).Code)

	for name, bad := range map[string]string{
		"acción inválida":   `{"action":"toggle","reason":"probar"}`,
		"campo desconocido": `{"action":"enable","reason":"probar","tenantId":"x"}`,
		"motivo corto":      `{"action":"enable","reason":"x"}`,
		"json roto":         `{"action":`,
		"sin cuerpo":        ``,
	} {
		assert.Equal(t, http.StatusBadRequest, e.call(t, a, authz.RoleTenantAdmin, http.MethodPut, "/api/v1/featuremgmt/flags/module.claims", bad).Code, name)
	}
	assert.Equal(t, http.StatusBadRequest, e.call(t, a, authz.RoleTenantAdmin, http.MethodPut, "/api/v1/featuremgmt/flags/MODULE.X", body).Code, "patrón de clave")
}

func TestOverridesAreTenantIsolatedAndAudited(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a, b := e.tenant("basico"), e.tenant("basico")
	e.platformSet(t, a, flags.CrmModule, boolPtr(true))

	count := func(tenant kernel.TenantID, q string) int {
		var n int
		ctx := reqctx.WithTenantID(context.Background(), tenant)
		require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error { return tx.QueryRow(ctx, q).Scan(&n) }))
		return n
	}
	assert.Equal(t, 1, count(a, `SELECT count(*) FROM featuremgmt.tenant_overrides`))
	assert.Equal(t, 0, count(b, `SELECT count(*) FROM featuremgmt.tenant_overrides`), "RLS")
	assert.Equal(t, 1, count(a, `SELECT count(*) FROM audit.events WHERE action = 'featuremgmt.flag.updated'
		AND resource_id = 'module.crm' AND details->>'new' = 'true' AND details->>'actor' = 'platform'`))
	assert.Equal(t, 0, count(b, `SELECT count(*) FROM audit.events`))
}

func TestGlobalKillSwitch(t *testing.T) {
	// Sin t.Parallel: el switch global afecta a todos los tenants de la suite.
	e := newEnv(t)
	a := e.tenant("profesional")
	ctx := reqctx.WithTenantPlan(reqctx.WithTenantID(context.Background(), a), "profesional")
	require.True(t, e.eval.IsEnabled(ctx, flags.ClaimsModule))

	require.NoError(t, e.mod.SetGlobalSwitch.Handle(context.Background(), app.SetGlobalSwitchCommand{Key: flags.ClaimsModule, Enabled: boolPtr(false), Reason: "incidente"}))
	assert.False(t, e.eval.IsEnabled(ctx, flags.ClaimsModule), "kill switch gana al plan")
	require.NoError(t, e.mod.SetGlobalSwitch.Handle(context.Background(), app.SetGlobalSwitchCommand{Key: flags.ClaimsModule, Reason: "resuelto"}))
	assert.True(t, e.eval.IsEnabled(ctx, flags.ClaimsModule))
}
