// Package reporting es el módulo de indicadores.
package reporting

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	reportinghttp "github.com/marvinrepremarca/seguros/internal/modules/reporting/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/reporting/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/reporting/app"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Deps son las dependencias del módulo.
type Deps struct {
	DB      *db.DB
	Checker flags.Checker
	Policy  *authz.Policy
	Clock   kernel.Clock
	Logger  *slog.Logger
}

// Module expone los indicadores.
type Module struct {
	server  *reportinghttp.Server
	checker flags.Checker
}

// New compone el módulo.
func New(d Deps) *Module {
	svc := &app.Service{Store: postgres.NewStore(d.DB), Checker: d.Checker, Clock: d.Clock}
	return &Module{server: reportinghttp.NewServer(svc, d.Policy, d.Logger), checker: d.Checker}
}

// Mount registra las rutas detrás del flag: apagado, responde 404.
func (m *Module) Mount(r chi.Router) (err error) {
	r.Group(func(g chi.Router) {
		g.Use(flags.Require(m.checker, flags.ReportingModule))
		err = m.server.Mount(g)
	})
	return err
}
