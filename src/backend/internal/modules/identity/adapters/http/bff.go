// Package identityhttp expone el BFF: login OIDC con Keycloak (Authorization Code + PKCE), sesión
// server-side y datos de la sesión para la SPA. Solo mapea HTTP ↔ casos de uso.
package identityhttp

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/app"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/domain"
	tenancy "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Códigos estables con los que el callback redirige a /login?error=<código>.
const (
	CodeLoginInvalidState = "identity.login.invalid_state"
	CodeLoginIDPError     = "identity.login.idp_error"
	CodeLoginFailed       = "identity.login.failed"
)

const maxReturnTo = 512

// IdentityProvider es el puerto hacia el proveedor OIDC (implementado por adapters/oidc).
type IdentityProvider interface {
	AuthCodeURL(state, nonce, verifier string) string
	Exchange(ctx context.Context, code, verifier, nonce string) (app.VerifiedIdentity, error)
	LogoutURL(idTokenHint string) string
	// Refresh confirma la sesión; app.ErrSessionRevoked si el proveedor ya no la reconoce.
	Refresh(ctx context.Context, refreshToken string) (app.VerifiedIdentity, error)
}

// LoginCompleter es el caso de uso que completa el login.
type LoginCompleter interface {
	Handle(ctx context.Context, cmd app.CompleteLoginCommand) (app.CompleteLoginResult, error)
}

// Options configura el servidor BFF.
type Options struct {
	Clock     kernel.Clock
	Provider  IdentityProvider
	Cipher    TokenCipher
	Login     LoginCompleter
	Tenants   tenancy.Directory
	Policy    *authz.Policy
	Sessions  *scs.SessionManager
	AuthTx    *scs.SessionManager
	Guard     *authz.OperationGuard
	PublicURL string
	Logger    *slog.Logger
}

// Server implementa StrictServerInterface.
type Server struct {
	opts Options
}

var _ StrictServerInterface = (*Server)(nil)

// NewServer construye el servidor BFF.
func NewServer(opts Options) *Server { return &Server{opts: opts} }

// NewGuard construye el guardia de permisos a partir del contrato embebido (x-permission).
func NewGuard(policy *authz.Policy) (*authz.OperationGuard, error) {
	spec, err := GetSpec()
	if err != nil {
		return nil, err
	}
	return authz.NewOperationGuard(spec, policy)
}

// Mount registra las rutas del BFF con su contrato (autorización por x-permission + validación de
// la petición). La sesión, el autenticador y CSRF los aplica cmd/api a todo el grupo; aquí se añade
// la sesión de la transacción de login.
func (s *Server) Mount(r chi.Router) error {
	spec, err := GetSpec()
	if err != nil {
		return err
	}
	contract, err := httpx.Contract(spec, s.opts.Guard.Check, s.opts.Logger)
	if err != nil {
		return err
	}
	guard := func(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			if err := s.opts.Guard.Check(ctx, operationID); err != nil {
				return nil, err
			}
			return f(ctx, w, r, req)
		}
	}
	h := NewStrictHandlerWithOptions(s, []StrictMiddlewareFunc{guard}, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  badRequest,
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) { httpx.WriteError(w, r, s.opts.Logger, err) },
	})
	r.Group(func(g chi.Router) {
		g.Use(contract, s.opts.AuthTx.LoadAndSave)
		HandlerWithOptions(h, ChiServerOptions{BaseRouter: g, ErrorHandlerFunc: badRequest})
	})
	return nil
}

func badRequest(w http.ResponseWriter, r *http.Request, _ error) {
	httpx.WriteProblem(w, r, http.StatusBadRequest, httpx.CodeBadRequest, "La solicitud no es válida")
}

// Login implementa StrictServerInterface.
func (s *Server) Login(ctx context.Context, req LoginRequestObject) (LoginResponseObject, error) {
	state, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	nonce, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	verifier, err := randomToken(48) // 64 caracteres: RFC 7636 exige 43–128
	if err != nil {
		return nil, err
	}
	if err := s.opts.AuthTx.RenewToken(ctx); err != nil {
		return nil, err
	}
	s.opts.AuthTx.Put(ctx, keyState, state)
	s.opts.AuthTx.Put(ctx, keyNonce, nonce)
	s.opts.AuthTx.Put(ctx, keyVerifier, verifier)
	s.opts.AuthTx.Put(ctx, keyReturnTo, sanitizeReturnTo(deref(req.Params.ReturnTo)))
	location := s.opts.Provider.AuthCodeURL(state, nonce, verifier)
	return Login302Response{Headers: Login302ResponseHeaders{Location: &location}}, nil
}

// LoginCallback implementa StrictServerInterface.
func (s *Server) LoginCallback(ctx context.Context, req LoginCallbackRequestObject) (LoginCallbackResponseObject, error) {
	state := s.opts.AuthTx.PopString(ctx, keyState)
	nonce := s.opts.AuthTx.PopString(ctx, keyNonce)
	verifier := s.opts.AuthTx.PopString(ctx, keyVerifier)
	returnTo := sanitizeReturnTo(s.opts.AuthTx.PopString(ctx, keyReturnTo))
	if err := s.opts.AuthTx.Destroy(ctx); err != nil {
		return nil, err
	}

	// El state se valida antes que cualquier otro parámetro (también en las respuestas de error,
	// RFC 6749 §4.1.2.1): un enlace forjado no puede cancelar ni contaminar el login de otro.
	if state == "" || !constantTimeEqual(state, deref(req.Params.State)) {
		s.opts.Logger.InfoContext(ctx, "callback de login con state inválido")
		return redirectPage(loginErrorURL(CodeLoginInvalidState))
	}
	if req.Params.Error != nil {
		s.opts.Logger.InfoContext(ctx, "login rechazado por el proveedor", "idp_error", truncate(*req.Params.Error, 64))
		return redirectPage(loginErrorURL(CodeLoginIDPError))
	}
	if deref(req.Params.Code) == "" {
		return redirectPage(loginErrorURL(CodeLoginInvalidState))
	}
	id, err := s.opts.Provider.Exchange(ctx, deref(req.Params.Code), verifier, nonce)
	if err != nil {
		s.opts.Logger.WarnContext(ctx, "canje OIDC fallido", "error", err)
		return redirectPage(loginErrorURL(CodeLoginFailed))
	}
	res, err := s.opts.Login.Handle(ctx, app.CompleteLoginCommand{
		Subject: id.Subject, Roles: id.Roles, Organizations: id.Organizations, AuthMethods: id.AuthMethods,
		Profile: profile(id),
	})
	if err != nil {
		if de, ok := kernel.AsDomainError(err); ok && de.Kind() == kernel.KindForbidden {
			s.opts.Logger.InfoContext(ctx, "login denegado", "code", de.Code())
			return redirectPage(loginErrorURL(de.Code()))
		}
		// El callback es una navegación del navegador: cualquier otro error también vuelve a /login.
		s.opts.Logger.ErrorContext(ctx, "completando login", "error", err)
		return redirectPage(loginErrorURL(CodeLoginFailed))
	}

	// Sesión nueva y vacía al autenticarse: sin datos heredados de una sesión previa y con token
	// nuevo (previene fijación de sesión).
	if err := s.opts.Sessions.Clear(ctx); err != nil {
		return nil, err
	}
	if err := s.opts.Sessions.RenewToken(ctx); err != nil {
		return nil, err
	}
	roles := make([]string, len(res.Roles))
	for i, r := range res.Roles {
		roles[i] = string(r)
	}
	raw, err := json.Marshal(storedPrincipal{
		UserID: uuid.UUID(res.UserID), TenantID: uuid.UUID(res.TenantID), Roles: roles, DisplayName: res.DisplayName,
	})
	if err != nil {
		return nil, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	refresh, err := sealRefresh(ctx, s.opts.Cipher, uuid.UUID(res.TenantID), uuid.UUID(res.UserID), id.RefreshToken)
	if err != nil {
		return nil, err
	}
	s.opts.Sessions.Put(ctx, keyPrincipal, string(raw))
	s.opts.Sessions.Put(ctx, keyCSRF, csrf)
	s.opts.Sessions.Put(ctx, keyRefresh, refresh)
	s.opts.Sessions.Put(ctx, keyRevalidatedAt, s.opts.Clock.Now().UTC().Format(time.RFC3339Nano))
	return redirectPage(returnTo)
}

// Logout implementa StrictServerInterface. El ID token nunca se guarda ni viaja al navegador: el
// cierre en el proveedor se hace con client_id + post_logout_redirect_uri (Keycloak pide
// confirmación al usuario).
func (s *Server) Logout(ctx context.Context, _ LogoutRequestObject) (LogoutResponseObject, error) {
	if err := s.opts.Sessions.Destroy(ctx); err != nil {
		return nil, err
	}
	return logoutResponse{LogoutResult{LogoutUrl: s.opts.Provider.LogoutURL("")}}, nil
}

// logoutResponse añade el borrado de la cookie anti-CSRF a la respuesta generada.
type logoutResponse struct{ body LogoutResult }

func (l logoutResponse) VisitLogoutResponse(w http.ResponseWriter) error {
	clearCSRFCookie(w)
	return Logout200JSONResponse(l.body).VisitLogoutResponse(w)
}

// GetSession implementa StrictServerInterface.
func (s *Server) GetSession(ctx context.Context, _ GetSessionRequestObject) (GetSessionResponseObject, error) {
	p, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return nil, authz.ErrUnauthenticated
	}
	roles := make([]string, 0, len(p.Roles))
	for _, r := range p.Roles {
		roles = append(roles, string(r))
	}
	perms := make([]string, 0)
	for _, perm := range p.Permissions() {
		perms = append(perms, string(perm))
	}
	return GetSession200JSONResponse{
		User:        SessionUser{Id: uuid.UUID(p.UserID), DisplayName: displayName(ctx, s.opts.Sessions)},
		Tenant:      SessionTenant{Id: uuid.UUID(p.TenantID), Plan: SessionTenantPlan(reqctx.TenantPlan(ctx))},
		Roles:       roles,
		Permissions: perms,
	}, nil
}

func profile(id app.VerifiedIdentity) domain.Profile {
	return domain.Profile{Email: id.Email, DisplayName: id.Name}
}

// redirectTemplate redirige con meta refresh desde la propia origin (ver contrato). html/template
// escapa la URL; la CSP de la API (default-src 'none') no impide el meta refresh.
var redirectTemplate = template.Must(template.New("redirect").Parse( //nolint:gochecknoglobals // plantilla inmutable
	`<!doctype html><html lang="es-CO"><head><meta charset="utf-8">` +
		`<meta http-equiv="refresh" content="0;url={{.}}"><title>Redirigiendo…</title></head>` +
		`<body><p><a href="{{.}}">Continuar</a></p></body></html>`))

func redirectPage(target string) (LoginCallbackResponseObject, error) {
	var buf bytes.Buffer
	if err := redirectTemplate.Execute(&buf, target); err != nil {
		return nil, err
	}
	return LoginCallback200TexthtmlResponse{Body: &buf, ContentLength: int64(buf.Len())}, nil
}

func loginErrorURL(code string) string { return "/login?error=" + url.QueryEscape(code) }

// sanitizeReturnTo acepta solo rutas relativas a la propia origin (evita open redirect).
func sanitizeReturnTo(s string) string {
	if s == "" || len(s) > maxReturnTo || !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") ||
		strings.ContainsAny(s, "\\") || strings.ContainsFunc(s, unicode.IsControl) {
		return "/"
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return "/"
	}
	return u.RequestURI()
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
