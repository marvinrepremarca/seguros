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
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"

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
	keyPrincipal = "principal"
	keyCSRF      = "csrf"
	keyState     = "state"
	keyNonce     = "nonce"
	keyVerifier  = "verifier"
	keyReturnTo  = "returnTo"
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

// Authenticator resuelve el principal de cada petición a partir de la sesión.
type Authenticator struct {
	sessions *scs.SessionManager
	tenants  tenancy.Directory
	policy   *authz.Policy
	logger   *slog.Logger
}

// NewAuthenticator construye el autenticador.
func NewAuthenticator(sessions *scs.SessionManager, tenants tenancy.Directory, policy *authz.Policy, logger *slog.Logger) *Authenticator {
	return &Authenticator{sessions: sessions, tenants: tenants, policy: policy, logger: logger}
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
		p := authz.NewPrincipal(kernel.UserID(sp.UserID), tenant.ID, authz.ParseRoles(sp.Roles), a.policy)
		ctx = reqctx.WithTenantPlan(authz.WithPrincipal(ctx, p), tenant.Plan)
		ensureCSRFCookie(w, r, a.sessions.GetString(ctx, keyCSRF))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
