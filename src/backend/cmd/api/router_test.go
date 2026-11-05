package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/billing"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog"
	cataloghttp "github.com/marvinrepremarca/seguros/internal/modules/catalog/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/claims"
	"github.com/marvinrepremarca/seguros/internal/modules/commissions"
	"github.com/marvinrepremarca/seguros/internal/modules/crm"
	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt"
	featuremgmthttp "github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/identity"
	identityhttp "github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/oidc"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/app"
	"github.com/marvinrepremarca/seguros/internal/modules/parties"
	partieshttp "github.com/marvinrepremarca/seguros/internal/modules/parties/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/policies"
	policieshttp "github.com/marvinrepremarca/seguros/internal/modules/policies/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/quoting"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals"
	tenancy "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

type noProvider struct{}

func (noProvider) AuthCodeURL(string, string, string, bool) string { return "https://idp.example/auth" }
func (noProvider) Exchange(context.Context, string, string, string) (app.VerifiedIdentity, error) {
	return app.VerifiedIdentity{}, oidc.ErrInvalidToken
}
func (noProvider) LogoutURL(string) string { return "https://idp.example/logout" }
func (noProvider) Refresh(context.Context, string) (app.VerifiedIdentity, error) {
	return app.VerifiedIdentity{}, app.ErrSessionRevoked
}

type noTenants struct{}

func (noTenants) ResolveByOrgAlias(context.Context, string) (tenancy.TenantInfo, error) {
	return tenancy.TenantInfo{}, tenancy.ErrTenantNotFound
}
func (noTenants) Get(context.Context, kernel.TenantID) (tenancy.TenantInfo, error) {
	return tenancy.TenantInfo{}, tenancy.ErrTenantNotFound
}

const publicURL = "https://app.example.test"

func testRouter(t *testing.T) *chi.Mux {
	t.Helper()
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	clock := kernel.SystemClock{}
	logger := slog.New(slog.DiscardHandler)
	idm, err := identity.New(identity.Deps{
		Clock: clock, Audit: audit.NewRecorder(clock), Tenants: noTenants{}, Policy: policy, Provider: noProvider{},
		SessionStore: memstore.New(), AuthTxStore: memstore.New(), Logger: logger,
		Auth: config.Auth{PublicURL: publicURL, SessionIdle: time.Minute, SessionLifetime: time.Hour},
	})
	require.NoError(t, err)
	reg, err := flags.LoadRegistry()
	require.NoError(t, err)
	fm := featuremgmt.New(featuremgmt.Deps{
		Registry: reg, Checker: allOff{}, Policy: policy, Clock: clock, Logger: logger, Tenants: noTenants{},
	})
	cat, err := catalog.New(catalog.Deps{Policy: policy, Clock: clock, Logger: logger})
	require.NoError(t, err)
	r, err := buildRouter(routerDeps{
		Logger: logger, Clock: clock, MaxBodyBytes: 1 << 20, Identity: idm, FeatureMgmt: fm, Catalog: cat,
		Parties:     parties.New(parties.Deps{Policy: policy, Clock: clock, Logger: logger}),
		Policies:    policies.New(policies.Deps{Policy: policy, Clock: clock, Logger: logger}),
		Billing:     billing.New(billing.Deps{Checker: allOff{}, Policy: policy, Clock: clock, Logger: logger}),
		Commissions: commissions.New(commissions.Deps{Checker: allOff{}, Policy: policy, Clock: clock, Logger: logger}),
		Renewals:    renewals.New(renewals.Deps{Checker: allOff{}, Policy: policy, Clock: clock, Logger: logger}),
		Crm:         crm.New(crm.Deps{Checker: allOff{}, Policy: policy, Clock: clock, Logger: logger}),
		Quoting:     quoting.New(quoting.Deps{Checker: allOff{}, Policy: policy, Clock: clock, Logger: logger}),
		Claims:      claims.New(claims.Deps{Checker: allOff{}, Policy: policy, Clock: clock, Logger: logger}),
	})
	require.NoError(t, err)
	return r
}

type allOff struct{}

func (allOff) IsEnabled(context.Context, flags.Key) bool { return false }

// contracts son los contratos OpenAPI montados en la API.
func contracts(t *testing.T) []*openapi3.T {
	t.Helper()
	var out []*openapi3.T
	for _, get := range []func() (*openapi3.T, error){
		identityhttp.GetSpec, featuremgmthttp.GetSpec, partieshttp.GetSpec, cataloghttp.GetSpec, policieshttp.GetSpec,
	} {
		spec, err := get()
		require.NoError(t, err)
		out = append(out, spec)
	}
	return out
}

// TestOperationsWithPermissionRejectPrincipalWithoutIt: con sesión pero sin el permiso, toda
// operación con x-permission fino responde 403 (antes de validar la petición o tocar datos).
func TestOperationsWithPermissionRejectPrincipalWithoutIt(t *testing.T) {
	t.Parallel()
	r := testRouter(t)
	// Principal sin permisos: ya ningún rol base carece de todos (SalesAgent registra clientes).
	noGrants, err := authz.NewPolicy(map[authz.Role][]authz.Permission{})
	require.NoError(t, err)
	tenant := kernel.TenantID(uuid.Must(uuid.NewV7()))
	p := authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), tenant, []authz.Role{authz.RoleSalesAgent}, noGrants)
	checked := 0
	for _, spec := range contracts(t) {
		for path, item := range spec.Paths.Map() {
			for method, op := range item.Operations() {
				perm, ok := op.Extensions[authz.XPermission].(string)
				require.True(t, ok, "%s sin x-permission", op.OperationID)
				if perm == authz.PermPublic || perm == authz.PermAuthenticated {
					continue
				}
				target := param.ReplaceAllString(path, "module.billing")
				req := httptest.NewRequestWithContext(authz.WithPrincipal(t.Context(), p), method, target, nil)
				req.Header.Set("Origin", publicURL)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				assert.Equal(t, http.StatusForbidden, rec.Code, "%s %s (%s) sin %s", method, path, op.OperationID, perm)
				checked++
			}
		}
	}
	assert.Positive(t, checked)
}

// publicRoutes es la lista blanca explícita de rutas sin sesión (rules/security.md §A01).
func publicRoutes() []string {
	return []string{
		"GET /health/live",
		"GET /health/ready",
		"GET /bff/auth/login",
		"GET /bff/auth/callback",
	}
}

var param = regexp.MustCompile(`\{[^}]+\}`) //nolint:gochecknoglobals // regexp de prueba

// TestEveryRouteRequiresSessionUnlessWhitelisted recorre el router real: toda ruta fuera de la
// lista blanca debe responder 401 sin sesión (deny-by-default). Una ruta nueva sin x-permission
// ni siquiera compila el guardia (authz.NewOperationGuard falla al arrancar).
func TestEveryRouteRequiresSessionUnlessWhitelisted(t *testing.T) {
	t.Parallel()
	r := testRouter(t)
	var seen []string
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		seen = append(seen, key)
		target := param.ReplaceAllString(route, "0192f3a4-1b2c-7d3e-8f40-123456789abc")
		req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
		req.Header.Set("Origin", publicURL)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if slices.Contains(publicRoutes(), key) {
			assert.NotEqual(t, http.StatusUnauthorized, rec.Code, "%s es pública", key)
			return nil
		}
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s debe exigir sesión", key)
		return nil
	})
	require.NoError(t, err)
	for _, p := range publicRoutes() {
		assert.Contains(t, seen, p, "la lista blanca no debe tener rutas inexistentes")
	}
}

func TestIsDocumentLookup(t *testing.T) {
	t.Parallel()
	const base = "/api/v1/parties/parties"
	cases := []struct {
		method, path string
		want         bool
	}{
		{http.MethodPost, base, true},
		{http.MethodPost, base + "/search-by-document", true},
		{http.MethodPost, base + "/id/document/reveal", true},
		{http.MethodPut, base + "/id", true},
		{http.MethodGet, base, false},
		{http.MethodGet, base + "/id", false},
		{http.MethodPost, base + "/id/consents", false},
		{http.MethodPost, "/api/v1/policies/policies", false},
	}
	for _, c := range cases {
		req := httptest.NewRequestWithContext(t.Context(), c.method, c.path, nil)
		assert.Equal(t, c.want, isDocumentLookup(req), "%s %s", c.method, c.path)
	}
}

// main.go debe pasar todos los módulos que monta el router: un módulo sin cablear tumba el arranque.
func TestMainWiresEveryRouterModule(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("main.go")
	require.NoError(t, err)
	for _, field := range []string{"Identity:", "FeatureMgmt:", "Parties:", "Catalog:", "Policies:", "Billing:", "Commissions:", "Renewals:", "Crm:", "Quoting:", "Claims:"} {
		assert.Contains(t, string(src), field)
	}
}
