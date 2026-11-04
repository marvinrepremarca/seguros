package identityhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityhttp "github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/oidc"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/app"
	tenancy "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/test/oidctest"
)

const (
	publicURL       = "https://app.example.test"
	revalidateEvery = 5 * time.Minute
)

type fakeTenants struct {
	mu     sync.Mutex
	tenant tenancy.TenantInfo
	// resolveErr simula una falla de la BD del directorio de tenants.
	resolveErr error
}

func (f *fakeTenants) failResolve(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolveErr = err
}

type fakeMembers struct {
	mu   sync.Mutex
	left []kernel.UserID
}

func (f *fakeMembers) MarkLeftOrganization(_ context.Context, _ kernel.TenantID, user kernel.UserID, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.left = append(f.left, user)
	return nil
}

func (f *fakeMembers) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.left)
}

func (f *fakeTenants) ResolveByOrgAlias(_ context.Context, alias string) (tenancy.TenantInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resolveErr != nil {
		return tenancy.TenantInfo{}, f.resolveErr
	}
	if alias != "agencia-x" {
		return tenancy.TenantInfo{}, tenancy.ErrTenantNotFound
	}
	return f.tenant, nil
}

func (f *fakeTenants) Get(_ context.Context, id kernel.TenantID) (tenancy.TenantInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != f.tenant.ID {
		return tenancy.TenantInfo{}, tenancy.ErrTenantNotFound
	}
	return f.tenant, nil
}

func (f *fakeTenants) suspend() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tenant.Active = false
}

type fakeLogin struct {
	result app.CompleteLoginResult
	err    error
	got    app.CompleteLoginCommand
}

func (f *fakeLogin) Handle(_ context.Context, cmd app.CompleteLoginCommand) (app.CompleteLoginResult, error) {
	f.got = cmd
	return f.result, f.err
}

type env struct {
	op      *oidctest.Server
	router  http.Handler
	tenants *fakeTenants
	login   *fakeLogin
	clock   *kernel.FakeClock
	members *fakeMembers
}

func newEnv(t *testing.T) *env {
	t.Helper()
	secret := oidctest.RandomString()
	op, err := oidctest.Start("seguros-bff", secret, time.Now) //nolint:forbidigo // go-oidc usa el reloj real
	require.NoError(t, err)
	t.Cleanup(op.Close)
	provider, err := oidc.New(context.Background(), config.Auth{
		Issuer: op.URL, ClientID: "seguros-bff", ClientSecret: config.NewSecret(secret), PublicURL: publicURL,
	}, oidc.NewHTTPClient())
	require.NoError(t, err)

	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	guard, err := identityhttp.NewGuard(policy)
	require.NoError(t, err)
	tenant := tenancy.TenantInfo{ID: kernel.TenantID(uuid.Must(uuid.NewV7())), Plan: "profesional", Active: true}
	tenants := &fakeTenants{tenant: tenant}
	login := &fakeLogin{result: app.CompleteLoginResult{
		UserID: kernel.UserID(uuid.Must(uuid.NewV7())), TenantID: tenant.ID, Plan: "profesional",
		Roles: []authz.Role{authz.RoleManager}, DisplayName: "Ana",
	}}
	members := &fakeMembers{}
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.NewFakeClock(time.Now().UTC()) //nolint:forbidigo // solo decide cuándo revalidar; go-oidc usa el reloj real
	sessions := identityhttp.NewSessionManager(identityhttp.SessionOptions{Store: memstore.New(), IdleTimeout: 30 * time.Minute, Lifetime: 8 * time.Hour})
	srv := identityhttp.NewServer(identityhttp.Options{
		Provider: provider, Login: login, Tenants: tenants, Policy: policy, Sessions: sessions,
		AuthTx: identityhttp.NewAuthTxManager(memstore.New()), Guard: guard, PublicURL: publicURL, Clock: clock, Logger: logger,
	})
	root := httpx.NewRouter(httpx.RouterOptions{Logger: logger, Clock: kernel.SystemClock{}, MaxBodyBytes: 1 << 20})
	root.Group(func(r chi.Router) {
		r.Use(sessions.LoadAndSave, identityhttp.NewAuthenticator(identityhttp.AuthenticatorOptions{
			Sessions: sessions, Tenants: tenants, Policy: policy, Provider: provider, Members: members, Clock: clock, Interval: revalidateEvery, Logger: logger,
		}).Middleware, identityhttp.CSRF(sessions, publicURL))
		require.NoError(t, srv.Mount(r))
	})
	return &env{op: op, router: root, tenants: tenants, login: login, clock: clock, members: members}
}

// browser guarda cookies manualmente: las cookies __Host- son Secure y httptest usa http.
type browser struct {
	cookies map[string]*http.Cookie
}

func newBrowser() *browser { return &browser{cookies: map[string]*http.Cookie{}} }

func (b *browser) do(t *testing.T, h http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	for _, c := range b.cookies {
		req.AddCookie(c)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	for _, c := range cookiesOf(rec) {
		if c.MaxAge < 0 || (!c.Expires.IsZero() && c.Expires.Before(time.Now())) { //nolint:forbidigo // expiración de cookie real
			delete(b.cookies, c.Name)
			continue
		}
		b.cookies[c.Name] = c
	}
	return rec
}

func cookiesOf(rec *httptest.ResponseRecorder) []*http.Cookie {
	return (&http.Response{Header: rec.Header()}).Cookies()
}

func cookie(t *testing.T, res *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range cookiesOf(res) {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("falta la cookie %s", name)
	return nil
}

var metaRefresh = regexp.MustCompile(`content="0;url=([^"]+)"`) //nolint:gochecknoglobals // regexp de prueba

func redirectTarget(t *testing.T, res *httptest.ResponseRecorder) string {
	t.Helper()
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Header().Get("Content-Type"), "text/html")
	body := res.Body.String()
	m := metaRefresh.FindStringSubmatch(body)
	require.NotNil(t, m, body)
	return strings.ReplaceAll(m[1], "&amp;", "&")
}

// loginFlow ejecuta login → autorización en el proveedor → callback y devuelve la respuesta del callback.
func (e *env) loginFlow(t *testing.T, b *browser, returnTo string, claims oidctest.Claims) *httptest.ResponseRecorder {
	t.Helper()
	res := b.do(t, e.router, http.MethodGet, "/bff/auth/login?returnTo="+url.QueryEscape(returnTo), nil)
	require.Equal(t, http.StatusFound, res.Code)
	authTx := cookie(t, res, identityhttp.AuthTxCookie)
	assert.True(t, authTx.HttpOnly && authTx.Secure)
	assert.Equal(t, http.SameSiteLaxMode, authTx.SameSite, "la vuelta del IdP es una navegación entre sitios")

	cb, err := e.op.Authorize(res.Header().Get("Location"), claims)
	require.NoError(t, err)
	return b.do(t, e.router, http.MethodGet, "/bff/auth/callback?"+cb.Encode(), nil)
}

func claims() oidctest.Claims {
	return oidctest.Claims{Subject: "kc-1", Email: "ana@example.test", Name: "Ana", Roles: []string{"Manager"}, Organizations: []string{"agencia-x"}, AMR: []string{"otp"}}
}

func TestLoginSessionAndLogout(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b := newBrowser()

	res := e.loginFlow(t, b, "/polizas?estado=vigente", claims())
	assert.Equal(t, "/polizas?estado=vigente", redirectTarget(t, res))
	sess := cookie(t, res, identityhttp.SessionCookie)
	assert.True(t, sess.HttpOnly, "HttpOnly")
	assert.True(t, sess.Secure, "Secure")
	assert.Equal(t, http.SameSiteStrictMode, sess.SameSite)
	assert.Equal(t, "/", sess.Path)
	assert.Empty(t, sess.Domain, "__Host- sin Domain")
	assert.NotContains(t, b.cookies, identityhttp.AuthTxCookie, "la transacción de login se destruye")
	assert.Equal(t, app.CompleteLoginCommand{
		Subject: "kc-1", Roles: []string{"Manager"}, Organizations: []string{"agencia-x"}, AuthMethods: []string{"otp"},
		Profile: e.login.got.Profile,
	}, e.login.got)
	assert.Equal(t, "ana@example.test", e.login.got.Profile.Email)

	res = b.do(t, e.router, http.MethodGet, "/bff/me", nil)
	require.Equal(t, http.StatusOK, res.Code)
	var me struct {
		User        struct{ ID, DisplayName string } `json:"user"`
		Tenant      struct{ ID, Plan string }        `json:"tenant"`
		Roles       []string                         `json:"roles"`
		Permissions []string                         `json:"permissions"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&me))
	assert.Equal(t, "Ana", me.User.DisplayName)
	assert.Equal(t, "profesional", me.Tenant.Plan)
	assert.Equal(t, []string{"Manager"}, me.Roles)
	// Los permisos salen de los roles según la política (crece con cada módulo).
	assert.Contains(t, me.Permissions, string(authz.FeatureFlagRead))
	assert.NotContains(t, me.Permissions, string(authz.FeatureFlagUpdate), "Manager no cambia flags")
	csrf := cookie(t, res, identityhttp.CSRFCookie)
	assert.False(t, csrf.HttpOnly, "la SPA debe leer el token anti-CSRF")
	assert.True(t, csrf.Secure)

	res = b.do(t, e.router, http.MethodPost, "/bff/auth/logout", map[string]string{"Origin": publicURL})
	assert.Equal(t, http.StatusForbidden, res.Code, "sin X-CSRF-Token")
	res = b.do(t, e.router, http.MethodPost, "/bff/auth/logout", map[string]string{"Origin": "https://evil.example", identityhttp.CSRFHeader: csrf.Value})
	assert.Equal(t, http.StatusForbidden, res.Code, "Origin ajeno")

	res = b.do(t, e.router, http.MethodPost, "/bff/auth/logout", map[string]string{"Origin": publicURL, identityhttp.CSRFHeader: csrf.Value})
	require.Equal(t, http.StatusOK, res.Code)
	var out struct {
		LogoutURL string `json:"logoutUrl"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	assert.True(t, strings.HasPrefix(out.LogoutURL, e.op.URL+"/logout?"))
	assert.NotContains(t, out.LogoutURL, "id_token_hint", "el ID token nunca llega al navegador")

	res = b.do(t, e.router, http.MethodGet, "/bff/me", nil)
	assert.Equal(t, http.StatusUnauthorized, res.Code, "la sesión se destruyó")
}

func TestOldSessionDataIsNotInheritedOnLogin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b := newBrowser()
	e.loginFlow(t, b, "/", claims())
	before := b.cookies[identityhttp.SessionCookie].Value
	res := e.loginFlow(t, b, "/", claims()) // segundo login sobre una sesión existente
	assert.NotEqual(t, before, cookie(t, res, identityhttp.SessionCookie).Value)
	require.Equal(t, http.StatusOK, b.do(t, e.router, http.MethodGet, "/bff/me", nil).Code)
}

func TestSessionTokenIsRenewedOnLogin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b := newBrowser()
	b.cookies[identityhttp.SessionCookie] = &http.Cookie{Name: identityhttp.SessionCookie, Value: "token-fijado-por-atacante"}
	res := e.loginFlow(t, b, "/", claims())
	assert.NotEqual(t, "token-fijado-por-atacante", cookie(t, res, identityhttp.SessionCookie).Value)
}

func TestMeWithoutSessionIs401(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := newBrowser().do(t, e.router, http.MethodGet, "/bff/me", nil)
	assert.Equal(t, http.StatusUnauthorized, res.Code)
	assert.Equal(t, httpx.ContentTypeProblem, res.Header().Get("Content-Type"))
}

func TestSuspendedTenantLosesSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b := newBrowser()
	e.loginFlow(t, b, "/", claims())
	require.Equal(t, http.StatusOK, b.do(t, e.router, http.MethodGet, "/bff/me", nil).Code)

	e.tenants.suspend()
	assert.Equal(t, http.StatusUnauthorized, b.do(t, e.router, http.MethodGet, "/bff/me", nil).Code)
}

func TestCallbackFailuresRedirectToLoginWithCode(t *testing.T) {
	t.Parallel()
	t.Run("state distinto", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		b := newBrowser()
		res := b.do(t, e.router, http.MethodGet, "/bff/auth/login", nil)
		cb, err := e.op.Authorize(res.Header().Get("Location"), claims())
		require.NoError(t, err)
		cb.Set("state", "manipulado")
		res = b.do(t, e.router, http.MethodGet, "/bff/auth/callback?"+cb.Encode(), nil)
		assert.Equal(t, "/login?error="+identityhttp.CodeLoginInvalidState, redirectTarget(t, res))
		assert.NotContains(t, b.cookies, identityhttp.SessionCookie)
	})
	t.Run("sin transacción de login (CSRF de login)", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		res := newBrowser().do(t, e.router, http.MethodGet, "/bff/auth/callback?code=x&state=y", nil)
		assert.Equal(t, "/login?error="+identityhttp.CodeLoginInvalidState, redirectTarget(t, res))
	})
	t.Run("error del proveedor", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		b := newBrowser()
		res := b.do(t, e.router, http.MethodGet, "/bff/auth/login", nil)
		u, err := url.Parse(res.Header().Get("Location"))
		require.NoError(t, err)
		res = b.do(t, e.router, http.MethodGet, "/bff/auth/callback?error=access_denied&state="+u.Query().Get("state"), nil)
		assert.Equal(t, "/login?error="+identityhttp.CodeLoginIDPError, redirectTarget(t, res))
	})
	t.Run("error forjado sin state válido", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		b := newBrowser()
		b.do(t, e.router, http.MethodGet, "/bff/auth/login", nil)
		res := b.do(t, e.router, http.MethodGet, "/bff/auth/callback?error=access_denied&state=x", nil)
		assert.Equal(t, "/login?error="+identityhttp.CodeLoginInvalidState, redirectTarget(t, res))
	})
	t.Run("error interno vuelve a /login", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		e.login.err = errors.New("bd caída")
		res := e.loginFlow(t, newBrowser(), "/", claims())
		assert.Equal(t, "/login?error="+identityhttp.CodeLoginFailed, redirectTarget(t, res))
	})
	t.Run("parámetro fuera de contrato", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		res := newBrowser().do(t, e.router, http.MethodGet, "/bff/auth/callback?state="+strings.Repeat("s", 300), nil)
		assert.Equal(t, http.StatusBadRequest, res.Code, "maxLength del contrato")
	})
	t.Run("token rechazado", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		c := claims()
		c.Overrides = map[string]any{"aud": "otro"}
		res := e.loginFlow(t, newBrowser(), "/", c)
		assert.Equal(t, "/login?error="+identityhttp.CodeLoginFailed, redirectTarget(t, res))
	})
	t.Run("login denegado por política", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		e.login.err = app.ErrMFARequired
		b := newBrowser()
		res := e.loginFlow(t, b, "/", claims())
		assert.Equal(t, "/login?error=identity.login.mfa_required", redirectTarget(t, res))
		assert.NotContains(t, b.cookies, identityhttp.SessionCookie)
	})
	t.Run("el callback no se puede repetir", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		b := newBrowser()
		res := b.do(t, e.router, http.MethodGet, "/bff/auth/login", nil)
		cb, err := e.op.Authorize(res.Header().Get("Location"), claims())
		require.NoError(t, err)
		authTx := b.cookies[identityhttp.AuthTxCookie]
		b.do(t, e.router, http.MethodGet, "/bff/auth/callback?"+cb.Encode(), nil)
		b.cookies[identityhttp.AuthTxCookie] = authTx // reenvía la cookie vieja
		delete(b.cookies, identityhttp.SessionCookie)
		res = b.do(t, e.router, http.MethodGet, "/bff/auth/callback?"+cb.Encode(), nil)
		assert.Equal(t, "/login?error="+identityhttp.CodeLoginInvalidState, redirectTarget(t, res))
	})
}

func TestSanitizeReturnTo(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"":                             "/",
		"/":                            "/",
		"/polizas/123?x=1":             "/polizas/123?x=1",
		"https://evil.example/":        "/",
		"//evil.example":               "/",
		"/\\evil.example":              "/",
		"javascript:alert(1)":          "/",
		"polizas":                      "/",
		"/ok\nSet-Cookie: x=1":         "/",
		"/" + strings.Repeat("a", 600): "/",
	}
	for in, want := range tests {
		assert.Equal(t, want, identityhttp.SanitizeReturnTo(in), "entrada %q", in)
	}
}

func meRoles(t *testing.T, e *env, b *browser) (int, []string) {
	t.Helper()
	res := b.do(t, e.router, http.MethodGet, "/bff/me", nil)
	if res.Code != http.StatusOK {
		return res.Code, nil
	}
	var me struct {
		Roles []string `json:"roles"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&me))
	return res.Code, me.Roles
}

// Una baja o un cambio en Keycloak se aplica como máximo en un intervalo de revalidación.
func TestSessionIsRevalidatedWithTheProvider(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*env)
		status int
		roles  []string
	}{
		{"usuario deshabilitado", func(e *env) { e.op.Revoke("kc-1") }, http.StatusUnauthorized, nil},
		{"sale de la organización", func(e *env) {
			c := claims()
			c.Organizations = []string{"otra-agencia"}
			e.op.SetClaims(c)
		}, http.StatusUnauthorized, nil},
		{"se queda sin roles", func(e *env) {
			c := claims()
			c.Roles = nil
			e.op.SetClaims(c)
		}, http.StatusUnauthorized, nil},
		{"pasa a un rol sin MFA nueva", func(e *env) {
			c := claims()
			c.Roles = []string{"Manager", "Operations"}
			e.op.SetClaims(c)
		}, http.StatusOK, []string{"Manager", "Operations"}},
		{"gana un rol que exige MFA: debe volver a entrar", func(e *env) {
			c := claims()
			c.Roles = []string{"Manager", "Accounting"}
			e.op.SetClaims(c)
		}, http.StatusUnauthorized, nil},
		{"pierde un rol", func(e *env) {
			c := claims()
			c.Roles = []string{"SalesAgent"}
			e.op.SetClaims(c)
		}, http.StatusOK, []string{"SalesAgent"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			b := newBrowser()
			e.loginFlow(t, b, "/", claims())
			c.change(e)

			e.clock.Advance(revalidateEvery - time.Second)
			status, roles := meRoles(t, e, b)
			assert.Equal(t, http.StatusOK, status, "dentro del intervalo no se consulta al proveedor")
			assert.Equal(t, []string{"Manager"}, roles)

			e.clock.Advance(time.Second)
			status, roles = meRoles(t, e, b)
			assert.Equal(t, c.status, status)
			assert.Equal(t, c.roles, roles)
		})
	}
}

func TestProviderOutageDoesNotCallTheProviderOnEveryRequest(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b := newBrowser()
	e.loginFlow(t, b, "/", claims())
	e.op.SetUnavailable(true)
	e.clock.Advance(revalidateEvery)
	before := e.op.Refreshes()

	for range 3 {
		status, _ := meRoles(t, e, b)
		require.Equal(t, http.StatusOK, status)
	}
	assert.Equal(t, before+1, e.op.Refreshes(), "tras la falla se espera antes de volver a intentar")

	e.clock.Advance(31 * time.Second)
	status, _ := meRoles(t, e, b)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, before+2, e.op.Refreshes(), "pasada la espera se reintenta")

	e.clock.Advance(3 * revalidateEvery)
	status, _ = meRoles(t, e, b)
	assert.Equal(t, http.StatusUnauthorized, status, "la espera no alarga la gracia")
}

func TestLeavingTheOrganizationIsRecorded(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b := newBrowser()
	e.loginFlow(t, b, "/", claims())
	c := claims()
	c.Organizations = []string{"otra-agencia"}
	e.op.SetClaims(c)
	e.clock.Advance(revalidateEvery)
	status, _ := meRoles(t, e, b)
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, 1, e.members.count())
}

// Una falla de la BD no se confunde con una salida de la organización.
func TestDirectoryFailureIsToleratedOnlyWithinTheGrace(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b := newBrowser()
	e.loginFlow(t, b, "/", claims())
	e.tenants.failResolve(errors.New("bd caída"))
	e.clock.Advance(revalidateEvery)
	status, _ := meRoles(t, e, b)
	assert.Equal(t, http.StatusOK, status)
	assert.Zero(t, e.members.count())

	e.clock.Advance(2 * revalidateEvery)
	status, _ = meRoles(t, e, b)
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Zero(t, e.members.count())
}

func TestProviderOutageIsToleratedOnlyWithinTheGrace(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b := newBrowser()
	e.loginFlow(t, b, "/", claims())
	e.op.SetUnavailable(true)

	e.clock.Advance(2 * revalidateEvery)
	status, _ := meRoles(t, e, b)
	assert.Equal(t, http.StatusOK, status, "un corte breve no expulsa a todos")

	e.op.SetUnavailable(false)
	e.clock.Advance(time.Second)
	status, _ = meRoles(t, e, b)
	require.Equal(t, http.StatusOK, status, "se recupera y reinicia el plazo")

	e.op.SetUnavailable(true)
	e.clock.Advance(3 * revalidateEvery)
	status, _ = meRoles(t, e, b)
	assert.Equal(t, http.StatusUnauthorized, status, "sin confirmación más allá de la gracia se cierra la sesión")
}

// scs guarda la sesión al escribir la respuesta: un cliente que ya se fue no es un error del servidor.
func TestSessionSaveFailureForAGoneClientIsNotAServerError(t *testing.T) {
	t.Parallel()
	var logs strings.Builder
	sessions := identityhttp.NewSessionManager(identityhttp.SessionOptions{
		Store: memstore.New(), IdleTimeout: time.Minute, Lifetime: time.Hour, Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	sessions.ErrorFunc(rec, httptest.NewRequestWithContext(gone, http.MethodGet, "/x", nil), errors.New("guardando: cliente desconectado"))
	assert.Equal(t, 499, rec.Code)
	assert.NotContains(t, logs.String(), "ERROR")

	rec = httptest.NewRecorder()
	sessions.ErrorFunc(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil), errors.New("valkey caído"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, logs.String(), "ERROR", "una falla real sí se registra")
}
