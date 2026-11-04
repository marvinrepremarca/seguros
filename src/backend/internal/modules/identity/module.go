// Package identity es el módulo núcleo de identidad: login OIDC con Keycloak vía BFF, sesión
// server-side en Valkey, aprovisionamiento JIT de usuarios y la identidad (Principal) de cada
// petición. No es apagable.
package identity

import (
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"

	identityhttp "github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/app"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	tenancy "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Deps son las dependencias del módulo.
type Deps struct {
	DB       *db.DB
	Clock    kernel.Clock
	Audit    *audit.Recorder
	Tenants  tenancy.Directory
	Policy   *authz.Policy
	Provider identityhttp.IdentityProvider
	// Cipher cifra el refresh token dentro de la sesión.
	Cipher identityhttp.TokenCipher
	// ActiveAgents dice qué asesores siguen activos (cuenta de servicio de Keycloak).
	ActiveAgents app.ActiveAgents
	// SessionStore guarda las sesiones (Valkey en producción; memoria en pruebas).
	SessionStore scs.Store
	// AuthTxStore guarda las transacciones de login.
	AuthTxStore scs.Store
	Auth        config.Auth
	Logger      *slog.Logger
}

// Module expone el BFF y los middlewares de sesión.
type Module struct {
	Server        *identityhttp.Server
	Directory     contracts.Directory
	Sessions      *scs.SessionManager
	authenticator *identityhttp.Authenticator
	publicURL     string
}

// New compone el módulo.
func New(d Deps) (*Module, error) {
	sessions := identityhttp.NewSessionManager(identityhttp.SessionOptions{
		Store: d.SessionStore, IdleTimeout: d.Auth.SessionIdle, Lifetime: d.Auth.SessionLifetime, Logger: d.Logger,
	})
	guard, err := identityhttp.NewGuard(d.Policy)
	if err != nil {
		return nil, err
	}
	store := postgres.NewStore(d.DB, d.Audit)
	login := app.NewCompleteLoginHandler(d.Tenants, store, d.Clock, d.Auth.MFARequired)
	return &Module{
		Directory: &app.AgentDirectory{Users: store, Active: d.ActiveAgents},
		Server: identityhttp.NewServer(identityhttp.Options{
			Provider: d.Provider, Cipher: d.Cipher, Login: login, Tenants: d.Tenants, Policy: d.Policy,
			Sessions: sessions, AuthTx: identityhttp.NewAuthTxManager(d.AuthTxStore), Guard: guard,
			PublicURL: d.Auth.PublicURL, Clock: d.Clock, Logger: d.Logger,
		}),
		Sessions: sessions,
		authenticator: identityhttp.NewAuthenticator(identityhttp.AuthenticatorOptions{
			Sessions: sessions, Tenants: d.Tenants, Policy: d.Policy, Provider: d.Provider, Members: store, Cipher: d.Cipher,
			Clock: d.Clock, Interval: d.Auth.SessionRevalidate, Logger: d.Logger,
		}),
		publicURL: d.Auth.PublicURL,
	}, nil
}

// Middlewares devuelve, en orden, la carga de sesión, la autenticación (Principal + tenant activo
// + plan) y la protección CSRF. cmd/api los aplica a todas las rutas /bff y /api.
func (m *Module) Middlewares() []func(http.Handler) http.Handler {
	return []func(http.Handler) http.Handler{
		m.Sessions.LoadAndSave,
		m.authenticator.Middleware,
		identityhttp.CSRF(m.Sessions, m.publicURL),
	}
}
