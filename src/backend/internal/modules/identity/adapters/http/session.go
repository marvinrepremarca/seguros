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
	"sync/atomic"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

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
	Logger      *slog.Logger
}

// NewSessionManager crea la sesión principal: cookie __Host- HttpOnly, Secure, SameSite=Strict,
// expiración por inactividad y absoluta (rules/security.md §Autenticación).
func NewSessionManager(opts SessionOptions) *scs.SessionManager {
	m := scs.New()
	m.Store = opts.Store
	m.IdleTimeout = opts.IdleTimeout
	m.Lifetime = opts.Lifetime
	m.Cookie = scs.SessionCookie{Name: SessionCookie, HttpOnly: true, Secure: true, Path: "/", SameSite: http.SameSiteStrictMode, Persist: false}
	m.ErrorFunc = sessionErrorFunc(opts.Logger)
	return m
}

// sessionErrorFunc responde los errores de la sesión como el resto de la API. scs guarda la sesión al
// escribir la respuesta: si el cliente ya se fue, ese guardado falla y no es una falla del servidor.
func sessionErrorFunc(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	if logger == nil {
		logger = slog.Default()
	}
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(r.Context().Err(), context.Canceled) {
			err = context.Canceled
		}
		httpx.WriteError(w, r, logger, err)
	}
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

// providerCooldown es cuánto se evita llamar al proveedor tras una falla, mientras la sesión siga
// dentro de la gracia: un corte de Keycloak no debe sumar la espera a cada petición.
const providerCooldown = 30 * time.Second

// MembershipRecorder registra que un usuario salió de la organización del tenant, para que deje de
// ofrecerse como asesor activo.
type MembershipRecorder interface {
	MarkLeftOrganization(ctx context.Context, tenant kernel.TenantID, user kernel.UserID, at time.Time) error
}

// AuthenticatorOptions configura el autenticador.
type AuthenticatorOptions struct {
	Sessions *scs.SessionManager
	Tenants  tenancy.Directory
	Policy   *authz.Policy
	Provider IdentityProvider
	Members  MembershipRecorder
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
	members  MembershipRecorder
	clock    kernel.Clock
	interval time.Duration
	logger   *slog.Logger
	refresh  singleflight.Group
	// providerDownUntil (UnixNano) marca hasta cuándo se evita llamar al proveedor.
	providerDownUntil atomic.Int64
}

// NewAuthenticator construye el autenticador.
func NewAuthenticator(o AuthenticatorOptions) *Authenticator {
	interval := o.Interval
	if interval <= 0 {
		interval = defaultRevalidateInterval
	}
	return &Authenticator{sessions: o.Sessions, tenants: o.Tenants, policy: o.Policy, provider: o.Provider, members: o.Members, clock: o.Clock, interval: interval, logger: o.Logger}
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
	withinGrace := age < revalidateGrace*a.interval
	if withinGrace && now.UnixNano() < a.providerDownUntil.Load() {
		return sp, true
	}
	id, err := a.refreshShared(ctx, token)
	switch {
	case errors.Is(err, app.ErrSessionRevoked):
		a.logClosed(ctx, sp, "revoked")
		return sp, false
	case err != nil:
		a.providerDownUntil.Store(now.Add(providerCooldown).UnixNano())
		a.logger.WarnContext(ctx, "no se pudo revalidar la sesión", "error", err)
		if !withinGrace {
			a.logClosed(ctx, sp, "provider_unavailable")
		}
		return sp, withinGrace
	}
	member, err := a.inTenantOrganization(ctx, id.Organizations, sp.TenantID)
	switch {
	case err != nil:
		// Una falla de la BD no es una salida de la organización: aplica la misma gracia.
		a.logger.WarnContext(ctx, "no se pudo resolver la organización de la sesión", "error", err)
		if !withinGrace {
			a.logClosed(ctx, sp, "directory_unavailable")
		}
		return sp, withinGrace
	case !member:
		a.logClosed(ctx, sp, "left_organization")
		a.recordLeft(ctx, sp)
		return sp, false
	}
	roles := authz.ParseRoles(id.Roles)
	if len(roles) == 0 {
		a.logClosed(ctx, sp, "no_roles")
		return sp, false
	}
	if gainsMFARole(authz.ParseRoles(sp.Roles), roles) {
		a.logClosed(ctx, sp, "mfa_role_gained")
		return sp, false
	}
	names := make([]string, len(roles))
	for i, r := range roles {
		names[i] = string(r)
	}
	if !slices.Equal(names, sp.Roles) {
		a.logger.InfoContext(ctx, "roles de la sesión actualizados", "user_id", sp.UserID.String(), "previous", sp.Roles, "new", names)
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

// refreshShared une las peticiones paralelas de una misma sesión en una sola llamada al proveedor. No
// depende de la cancelación de quien la inició: su cliente puede irse sin afectar a las demás.
func (a *Authenticator) refreshShared(ctx context.Context, token string) (app.VerifiedIdentity, error) {
	v, err, _ := a.refresh.Do(token, func() (any, error) {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revalidateTimeout)
		defer cancel()
		return a.provider.Refresh(cctx, token)
	})
	if err != nil {
		return app.VerifiedIdentity{}, err
	}
	id, ok := v.(app.VerifiedIdentity)
	if !ok {
		return app.VerifiedIdentity{}, errors.New("identity: resultado inesperado del proveedor")
	}
	return id, nil
}

func (a *Authenticator) logClosed(ctx context.Context, sp storedPrincipal, reason string) {
	a.logger.InfoContext(ctx, "sesión cerrada por revalidación", "user_id", sp.UserID.String(), "tenant_id", sp.TenantID.String(), "reason", reason)
}

// recordLeft no impide cerrar la sesión si falla: el cierre es el control, esto solo mantiene la lista.
func (a *Authenticator) recordLeft(ctx context.Context, sp storedPrincipal) {
	if a.members == nil {
		return
	}
	if err := a.members.MarkLeftOrganization(ctx, kernel.TenantID(sp.TenantID), kernel.UserID(sp.UserID), a.clock.Now()); err != nil {
		a.logger.WarnContext(ctx, "registrando la salida de la organización", "error", err)
	}
}

// inTenantOrganization indica si alguna organización del usuario sigue vinculada al tenant de la sesión.
// Solo "no vinculada" descarta una organización; cualquier otro error se devuelve.
func (a *Authenticator) inTenantOrganization(ctx context.Context, orgs []string, tenant uuid.UUID) (bool, error) {
	var failure error
	for _, org := range orgs {
		info, err := a.tenants.ResolveByOrgAlias(ctx, org)
		switch {
		case errors.Is(err, tenancy.ErrTenantNotFound):
			continue
		case err != nil:
			failure = err
			continue
		}
		if uuid.UUID(info.ID) == tenant {
			return true, nil
		}
	}
	return false, failure
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
