// Package quoting es el módulo apagable de cotización (Flujo 2).
package quoting

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	catalog "github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	identity "github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	quotingevents "github.com/marvinrepremarca/seguros/internal/modules/quoting/adapters/events"
	quotinghttp "github.com/marvinrepremarca/seguros/internal/modules/quoting/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/quoting/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/quoting/app"
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
	Outbox  *outbox.Writer
	Scope   parties.AgentScope
	Parties parties.Directory
	Users   identity.Directory
	Catalog catalog.Directory
	Schemas catalog.RiskSchemas
	Lines   catalog.LineSchemas
	Checker flags.Checker
	Policy  *authz.Policy
	Clock   kernel.Clock
	Logger  *slog.Logger
}

// Module expone las rutas.
type Module struct {
	server  *quotinghttp.Server
	checker flags.Checker
}

// New compone el módulo.
func New(d Deps) *Module {
	svc := &app.Service{Store: postgres.NewStore(d.DB, d.Audit, d.Outbox), Scope: d.Scope, Parties: d.Parties, Users: d.Users, Catalog: d.Catalog, Schemas: d.Schemas, Lines: d.Lines, Clock: d.Clock}
	return &Module{server: quotinghttp.NewServer(svc, d.Policy, d.Logger, d.Clock), checker: d.Checker}
}

// Mount registra las rutas detrás del flag: apagado, responde 404.
func (m *Module) Mount(r chi.Router) (err error) {
	r.Group(func(g chi.Router) {
		g.Use(flags.Require(m.checker, flags.QuotingModule))
		err = m.server.Mount(g)
	})
	return err
}

// Consumers devuelve los consumidores (los registra cmd/worker).
func Consumers(recorder *audit.Recorder) []outbox.Consumer {
	return quotingevents.Consumers(recorder)
}
