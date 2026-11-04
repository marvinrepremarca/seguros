package identityhttp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/app"
	tenancy "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Nombres de cookie. El prefijo __Host- obliga a Secure, Path=/ y sin Domain.
const (
	SessionCookie = "__Host-seguros_session"
	AuthTxCookie  = "__Host-seguros_auth"
	CSRFCookie    = "__Host-csrf"
	CSRFHeader    = "X-CSRF-Token"
)

// Claves dentro de la sesión.
const (
	keyPrincipal     = "principal"
	keyCSRF          = "csrf"
	keyState         = "state"
	keyNonce         = "nonce"
	keyVerifier      = "verifier"
	keyReturnTo      = "returnTo"
	keyRefresh       = "refresh"
	keyRevalidatedAt = "revalidatedAt"
)

// CodeCSRF es el código del error de CSRF (403).
const CodeCSRF = "identity.session.csrf"

// SessionOptions configura las sesiones del BFF.
type SessionOptions struct {
	Store       scs.Store
	IdleTimeout time.Duration
	Lifetime    time.Duration
}

// NewSessionManager crea la sesión principal: cookie __Host- HttpOnly, Secure, SameSite=Strict,
// expiración por inactividad y absoluta (rules/security.md §Autenticación).
func NewSessionManager(opts SessionOptions) *scs.SessionManager {
	m := scs.New()
	m.Store = opts.Store
	m.IdleTimeout = opts.IdleTimeout
	m.Lifetime = opts.Lifetime
	m.Cookie = scs.SessionCookie{Name: SessionCookie, HttpOnly: true, Secure: true, Path: "/", SameSite: http.SameSiteStrictMode, Persist: false}
	return m
}

// NewAuthTxManager crea la sesión de la transacción de login (state, nonce, verificador PKCE).
// Usa SameSite=Lax porque la vuelta desde el proveedor es una navegación entre sitios, en la que el
// navegador no envía cookies Strict. Vive 10 minutos y se destruye en el callback.
func NewAuthTxManager(store scs.Store) *scs.SessionManager {
	m := scs.New()
	m.Store = store
	m.Lifetime = 10 * time.Minute
	m.Cookie = scs.SessionCookie{Name: AuthTxCookie, HttpOnly: true, Secure: true, Path: "/", SameSite: http.SameSiteLaxMode, Persist: false}
	return m
}

// storedPrincipal es la identidad guardada en la sesión (server-side).
type storedPrincipal struct {
	UserID      uuid.UUID `json:"u"`
	TenantID    uuid.UUID `json:"t"`
	Roles       []string  `json:"r"`
	DisplayName string    `json:"n"`
}

// defaultRevalidateInterval es el plazo máximo para aplicar una baja o un cambio de rol.
const defaultRevalidateInterval = 5 * time.Minute

// revalidateGrace es cuántos intervalos se tolera sin respuesta del proveedor antes de cerrar la
// sesión: un corte breve de Keycloak no expulsa a todos, pero tampoco deja el control desactivado.
const revalidateGrace = 3

// revalidateTimeout acota la espera de la petición por el proveedor.
const revalidateTimeout = 5 * time.Second

// AuthenticatorOptions configura el autenticador.
type AuthenticatorOptions struct {
	Sessions *scs.SessionManager
	Tenants  tenancy.Directory
	Policy   *authz.Policy
	Provider IdentityProvider
	Clock    kernel.Clock
	Interval time.Duration
	Logger   *slog.Logger
}

// Authenticator resuelve el principal de cada petición a partir de la sesión.
type Authenticator struct {
	sessions *scs.SessionManager
	tenants  tenancy.Directory
	policy   *authz.Policy
	provider IdentityProvider
	clock    kernel.Clock
	interval time.Duration
	logger   *slog.Logger
}

// NewAuthenticator construye el autenticador.
func NewAuthenticator(o AuthenticatorOptions) *Authenticator {
	interval := o.Interval
	if interval <= 0 {
		interval = defaultRevalidateInterval
	}
	return &Authenticator{sessions: o.Sessions, tenants: o.Tenants, policy: o.Policy, provider: o.Provider, clock: o.Clock, interval: interval, logger: o.Logger}
}

// Middleware pone en el contexto el principal de la sesión. En cada petición revalida que el
// tenant siga activo y toma su plan vigente; si el tenant se suspendió, destruye la sesión. Sin
// sesión la petición sigue sin principal: el guardia de cada operación decide (401/404).
// Debe ir después de sessions.LoadAndSave.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sp, ok := a.load(ctx)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		tenant, err := a.tenants.Get(ctx, kernel.TenantID(sp.TenantID))
		switch {
		case errors.Is(err, tenancy.ErrTenantNotFound) || (err == nil && !tenant.Active):
			if derr := a.sessions.Destroy(ctx); derr != nil {
				a.logger.WarnContext(ctx, "destruyendo sesión de tenant inactivo", "error", derr)
			}
			next.ServeHTTP(w, r)
			return
		case err != nil:
			httpx.WriteError(w, r, a.logger, err)
			return
		}
		sp, ok = a.revalidate(ctx, sp)
		if !ok {
			if derr := a.sessions.Destroy(ctx); derr != nil {
				a.logger.WarnContext(ctx, "destruyendo sesión revocada", "error", derr)
			}
			next.ServeHTTP(w, r)
			return
		}
		p := authz.NewPrincipal(kernel.UserID(sp.UserID), tenant.ID, authz.ParseRoles(sp.Roles), a.policy)
		ctx = reqctx.WithTenantPlan(authz.WithPrincipal(ctx, p), tenant.Plan)
		ensureCSRFCookie(w, r, a.sessions.GetString(ctx, keyCSRF))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// revalidate confirma la sesión con el proveedor cada intervalo y aplica los roles vigentes. Devuelve
// false si la sesión debe cerrarse: usuario deshabilitado, fuera de la organización, sin roles, con un
// rol nuevo que exige MFA (el login es quien la verifica) o proveedor sin responder más allá de la gracia.
func (a *Authenticator) revalidate(ctx context.Context, sp storedPrincipal) (storedPrincipal, bool) {
	token := a.sessions.GetString(ctx, keyRefresh)
	if token == "" {
		return sp, false
	}
	now := a.clock.Now()
	// Texto, no time.Time: la sesión se serializa con gob y no registra ese tipo.
	last, err := time.Parse(time.RFC3339Nano, a.sessions.GetString(ctx, keyRevalidatedAt))
	if err != nil {
		last = time.Time{} // sin fecha válida se revalida ya
	}
	age := now.Sub(last)
	if age < a.interval {
		return sp, true
	}
	cctx, cancel := context.WithTimeout(ctx, revalidateTimeout)
	defer cancel()
	id, err := a.provider.Refresh(cctx, token)
	switch {
	case errors.Is(err, app.ErrSessionRevoked):
		a.logger.InfoContext(ctx, "sesión revocada en el proveedor")
		return sp, false
	case err != nil:
		a.logger.WarnContext(ctx, "no se pudo revalidar la sesión", "error", err)
		return sp, age < revalidateGrace*a.interval
	}
	if !a.inTenantOrganization(ctx, id.Organizations, sp.TenantID) {
		return sp, false
	}
	roles := authz.ParseRoles(id.Roles)
	if len(roles) == 0 || gainsMFARole(authz.ParseRoles(sp.Roles), roles) {
		return sp, false
	}
	names := make([]string, len(roles))
	for i, r := range roles {
		names[i] = string(r)
	}
	sp.Roles = names
	raw, err := json.Marshal(sp)
	if err != nil {
		a.logger.ErrorContext(ctx, "guardando la sesión revalidada", "error", err)
		return sp, false
	}
	a.sessions.Put(ctx, keyPrincipal, string(raw))
	a.sessions.Put(ctx, keyRevalidatedAt, now.UTC().Format(time.RFC3339Nano))
	if id.RefreshToken != "" {
		a.sessions.Put(ctx, keyRefresh, id.RefreshToken)
	}
	return sp, true
}

// inTenantOrganization indica si alguna organización del usuario sigue vinculada al tenant de la sesión.
func (a *Authenticator) inTenantOrganization(ctx context.Context, orgs []string, tenant uuid.UUID) bool {
	for _, org := range orgs {
		info, err := a.tenants.ResolveByOrgAlias(ctx, org)
		if err != nil {
			continue
		}
		if uuid.UUID(info.ID) == tenant {
			return true
		}
	}
	return false
}

func gainsMFARole(before, after []authz.Role) bool {
	for _, r := range authz.MFARoles() {
		if slices.Contains(after, r) && !slices.Contains(before, r) {
			return true
		}
	}
	return false
}

func (a *Authenticator) load(ctx context.Context) (storedPrincipal, bool) {
	raw := a.sessions.GetString(ctx, keyPrincipal)
	if raw == "" {
		return storedPrincipal{}, false
	}
	var sp storedPrincipal
	if err := json.Unmarshal([]byte(raw), &sp); err != nil || sp.UserID == uuid.Nil || sp.TenantID == uuid.Nil {
		return storedPrincipal{}, false
	}
	return sp, true
}

func displayName(ctx context.Context, sessions *scs.SessionManager) string {
	var sp storedPrincipal
	if err := json.Unmarshal([]byte(sessions.GetString(ctx, keyPrincipal)), &sp); err != nil {
		return ""
	}
	return sp.DisplayName
}

// CSRF protege los métodos que modifican: exige que Origin (o, en su defecto, Sec-Fetch-Site)
// corresponda a la origin pública y, si hay sesión, que X-CSRF-Token coincida con el token de la
// sesión (comparación en tiempo constante). Complementa SameSite=Strict.
func CSRF(sessions *scs.SessionManager, publicURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if !sameOrigin(r, publicURL) {
				httpx.WriteProblem(w, r, http.StatusForbidden, CodeCSRF, "Origen no permitido")
				return
			}
			want := sessions.GetString(r.Context(), keyCSRF)
			got := r.Header.Get(CSRFHeader)
			if want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
				httpx.WriteProblem(w, r, http.StatusForbidden, CodeCSRF, "Token anti-CSRF inválido")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func sameOrigin(r *http.Request, publicURL string) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		return origin == publicURL
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin"
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		u, err := url.Parse(ref)
		return err == nil && u.Scheme+"://"+u.Host == publicURL
	}
	// Clientes no navegador (sin Origin ni Sec-Fetch-*) no portan la cookie de sesión de un
	// navegador; el token de sesión sigue siendo obligatorio si hay sesión.
	return true
}

// ensureCSRFCookie publica el token anti-CSRF de la sesión en una cookie legible por la SPA (no
// es una credencial: sin la cookie de sesión HttpOnly no sirve).
func ensureCSRFCookie(w http.ResponseWriter, r *http.Request, token string) {
	if token == "" {
		return
	}
	if c, err := r.Cookie(CSRFCookie); err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) == 1 {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CSRFCookie, Value: token, Path: "/", Secure: true, SameSite: http.SameSiteStrictMode}) // #nosec G124 -- la SPA debe leerla (no HttpOnly)
}

func clearCSRFCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CSRFCookie, Value: "", Path: "/", MaxAge: -1, Secure: true, SameSite: http.SameSiteStrictMode}) // #nosec G124 -- borrado
}

// randomToken devuelve n bytes aleatorios (crypto/rand) en base64url.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
