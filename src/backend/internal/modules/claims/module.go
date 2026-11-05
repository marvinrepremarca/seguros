// Package claims es el módulo apagable de siniestros (Flujo 7).
package claims

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	claimshttp "github.com/marvinrepremarca/seguros/internal/modules/claims/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/claims/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/claims/app"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

// Deps son las dependencias del módulo.
type Deps struct {
	DB       *db.DB
	Audit    *audit.Recorder
	Outbox   *outbox.Writer
	Policies policies.Directory
	Parties  parties.Directory
	Cipher   app.Cipher
	Checker  flags.Checker
	Policy   *authz.Policy
	Clock    kernel.Clock
	Logger   *slog.Logger
}

// Module expone las rutas.
type Module struct {
	server  *claimshttp.Server
	checker flags.Checker
}

// New compone el módulo.
func New(d Deps) *Module {
	svc := &app.Service{Store: postgres.NewStore(d.DB, d.Audit, d.Outbox), Policies: d.Policies, Parties: d.Parties, Cipher: d.Cipher, Clock: d.Clock}
	return &Module{server: claimshttp.NewServer(svc, d.Policy, d.Logger), checker: d.Checker}
}

// Mount registra las rutas detrás del flag: apagado, responde 404.
func (m *Module) Mount(r chi.Router) (err error) {
	r.Group(func(g chi.Router) {
		g.Use(flags.Require(m.checker, flags.ClaimsModule))
		err = m.server.Mount(g)
	})
	return err
}
