// Package featuremgmt es el módulo núcleo de feature flags y entitlements por tenant: persiste
// overrides y switches globales, expone el estado a la SPA y aplica los cambios con validación de
// dependencias y auditoría. La evaluación la hace internal/platform/flags. No es apagable.
package featuremgmt

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	featuremgmthttp "github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/app"
	tenancy "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Store es la persistencia del módulo; también es el flags.Store del evaluador de plataforma.
type Store = postgres.Store

// NewStore construye la persistencia (se crea antes que el evaluador, que la necesita).
func NewStore(database *db.DB, recorder *audit.Recorder) *Store {
	return postgres.NewStore(database, recorder)
}

// Deps son las dependencias del módulo.
type Deps struct {
	Store       *Store
	Tenants     tenancy.Directory
	Registry    *flags.Registry
	Invalidator app.Invalidator
	Checker     flags.Checker
	Policy      *authz.Policy
	Clock       kernel.Clock
	Logger      *slog.Logger
}

// Module expone el servidor HTTP y las operaciones de plataforma.
type Module struct {
	server *featuremgmthttp.Server
	// PlatformSetFlag cambia overrides como super-administrador (solo cmd/platformctl).
	PlatformSetFlag *app.SetTenantFlagHandler
	// SetGlobalSwitch cambia kill switches globales (solo cmd/platformctl).
	SetGlobalSwitch *app.SetGlobalSwitchHandler
}

// New compone el módulo.
func New(d Deps) *Module {
	deps := app.NewDeps(d.Registry, d.Store, d.Tenants, d.Invalidator, d.Clock, d.Logger)
	return &Module{
		server: featuremgmthttp.NewServer(featuremgmthttp.Options{
			List: app.NewListTenantFlagsHandler(deps), Set: app.NewSetTenantFlagHandler(deps, app.ActorTenantAdmin),
			Checker: d.Checker, Reg: d.Registry, Policy: d.Policy, Logger: d.Logger,
		}),
		PlatformSetFlag: app.NewSetTenantFlagHandler(deps, app.ActorPlatform),
		SetGlobalSwitch: app.NewSetGlobalSwitchHandler(deps),
	}
}

// Mount registra las rutas del módulo.
func (m *Module) Mount(r chi.Router) error { return m.server.Mount(r) }
