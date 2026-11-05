// Package commissions es el módulo apagable de comisiones (Flujo 9). Requiere module.billing.
package commissions

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	catalog "github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	commissionsevents "github.com/marvinrepremarca/seguros/internal/modules/commissions/adapters/events"
	commissionshttp "github.com/marvinrepremarca/seguros/internal/modules/commissions/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/commissions/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/commissions/app"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

// Deps son las dependencias del módulo.
type Deps struct {
	DB      *db.DB
	Audit   *audit.Recorder
	Checker flags.Checker
	Policy  *authz.Policy
	Clock   kernel.Clock
	Logger  *slog.Logger
}

// Module expone las rutas.
type Module struct {
	server  *commissionshttp.Server
	checker flags.Checker
}

// New compone el módulo.
func New(d Deps) *Module {
	svc := &app.Service{Store: postgres.NewStore(d.DB, d.Audit), Clock: d.Clock}
	return &Module{server: commissionshttp.NewServer(svc, d.Policy, d.Logger), checker: d.Checker}
}

// Mount registra las rutas detrás del flag: apagado, responde 404.
func (m *Module) Mount(r chi.Router) (err error) {
	r.Group(func(g chi.Router) {
		g.Use(flags.Require(m.checker, flags.CommissionsModule))
		err = m.server.Mount(g)
	})
	return err
}

// Consumers devuelve los consumidores de eventos (los registra cmd/worker).
func Consumers(database *db.DB, recorder *audit.Recorder, directory catalog.Directory, clock kernel.Clock) []outbox.Consumer {
	return commissionsevents.Consumers(postgres.NewStore(database, recorder), &app.Reactor{Catalog: directory, Clock: clock})
}
