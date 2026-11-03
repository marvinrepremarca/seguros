package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/identity"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/oidc"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/app"
	tenancy "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

type noProvider struct{}

func (noProvider) AuthCodeURL(string, string, string) string { return "https://idp.example/auth" }
func (noProvider) Exchange(context.Context, string, string, string) (app.VerifiedIdentity, error) {
	return app.VerifiedIdentity{}, oidc.ErrInvalidToken
}
func (noProvider) LogoutURL(string) string { return "https://idp.example/logout" }

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
	return buildRouter(routerDeps{Logger: logger, Clock: clock, MaxBodyBytes: 1 << 20, Identity: idm})
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
