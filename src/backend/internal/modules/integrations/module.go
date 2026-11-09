// Package integrations es la capa anticorrupción con las aseguradoras.
package integrations

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	integrationshttp "github.com/marvinrepremarca/seguros/internal/modules/integrations/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/integrations/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/integrations/adapters/resilience"
	"github.com/marvinrepremarca/seguros/internal/modules/integrations/adapters/simulator"
	"github.com/marvinrepremarca/seguros/internal/modules/integrations/app"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// SimulatorConnector es la aseguradora simulada. Nunca se registra en staging ni producción.
func SimulatorConnector() app.Connector {
	return app.Connector{Key: "simulator", Name: "Aseguradora simulada", Simulated: true, KillSwitch: flags.OpsIntegrationsSimulator, Gateway: simulator.Gateway{}}
}

// Deps son las dependencias del módulo.
type Deps struct {
	DB         *db.DB
	Connectors []app.Connector
	Checker    flags.Checker
	Policy     *authz.Policy
	Clock      kernel.Clock
	Logger     *slog.Logger
}

// Module expone los conectores.
type Module struct {
	server  *integrationshttp.Server
	checker flags.Checker
}

// New compone el módulo.
func New(d Deps) *Module {
	connectors := make([]app.Connector, 0, len(d.Connectors))
	for _, c := range d.Connectors {
		c.Gateway = resilience.WithBreaker(c.Key, c.Gateway)
		connectors = append(connectors, c)
	}
	svc := app.NewService(connectors, d.Checker, postgres.NewStore(d.DB), d.Clock)
	return &Module{server: integrationshttp.NewServer(svc, d.Policy, d.Logger), checker: d.Checker}
}

// Mount registra las rutas detrás del flag: apagado, responde 404.
func (m *Module) Mount(r chi.Router) (err error) {
	r.Group(func(g chi.Router) {
		g.Use(flags.Require(m.checker, flags.IntegrationsModule))
		err = m.server.Mount(g)
	})
	return err
}
