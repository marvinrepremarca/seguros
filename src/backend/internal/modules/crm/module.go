// Package crm es el módulo apagable de prospectos (Flujo 1).
package crm

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	crmevents "github.com/marvinrepremarca/seguros/internal/modules/crm/adapters/events"
	crmhttp "github.com/marvinrepremarca/seguros/internal/modules/crm/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/crm/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/crm/app"
	identity "github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
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
	Scope   parties.AgentScope
	Users   identity.Directory
	Checker flags.Checker
	Policy  *authz.Policy
	Clock   kernel.Clock
	Logger  *slog.Logger
}

// Module expone las rutas.
type Module struct {
	server  *crmhttp.Server
	checker flags.Checker
}

// New compone el módulo.
func New(d Deps) *Module {
	svc := &app.Service{Store: postgres.NewStore(d.DB, d.Audit), Scope: d.Scope, Users: d.Users, Clock: d.Clock}
	return &Module{server: crmhttp.NewServer(svc, d.Policy, d.Logger), checker: d.Checker}
}

// Mount registra las rutas detrás del flag: apagado, responde 404.
func (m *Module) Mount(r chi.Router) (err error) {
	r.Group(func(g chi.Router) {
		g.Use(flags.Require(m.checker, flags.CrmModule))
		err = m.server.Mount(g)
	})
	return err
}

// Consumers devuelve los consumidores (los registra cmd/worker).
func Consumers(database *db.DB, recorder *audit.Recorder, clock kernel.Clock) []outbox.Consumer {
	return crmevents.Consumers(postgres.NewStore(database, recorder), recorder, clock)
}
