// Package compliance es el módulo de cumplimiento (Flujo 10).
package compliance

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	compliancehttp "github.com/marvinrepremarca/seguros/internal/modules/compliance/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/compliance/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/compliance/app"
	"github.com/marvinrepremarca/seguros/internal/modules/compliance/contracts"
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
	Outbox  *outbox.Writer
	Parties parties.ComplianceView
	Cipher  app.Cipher
	Checker flags.Checker
	Policy  *authz.Policy
	Clock   kernel.Clock
	Logger  *slog.Logger
}

// Module expone las rutas y la consulta de KYC.
type Module struct {
	server  *compliancehttp.Server
	checker flags.Checker
	// Statuses responde a Policies si un tercero tiene KYC aprobado.
	Statuses contracts.KycStatuses
}

// New compone el módulo.
func New(d Deps) *Module {
	svc := &app.Service{Store: postgres.NewStore(d.DB, d.Audit, d.Outbox), Parties: d.Parties, Cipher: d.Cipher, Clock: d.Clock}
	return &Module{server: compliancehttp.NewServer(svc, d.Policy, d.Logger), checker: d.Checker, Statuses: app.Statuses{Service: svc}}
}

// Mount registra las rutas detrás del flag: apagado, responde 404.
func (m *Module) Mount(r chi.Router) (err error) {
	r.Group(func(g chi.Router) {
		g.Use(flags.Require(m.checker, flags.ComplianceModule))
		err = m.server.Mount(g)
	})
	return err
}
