// Package renewals es el módulo apagable de renovaciones (Flujo 6).
package renewals

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/riverqueue/river"

	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	renewalsevents "github.com/marvinrepremarca/seguros/internal/modules/renewals/adapters/events"
	renewalshttp "github.com/marvinrepremarca/seguros/internal/modules/renewals/adapters/http"
	renewalsjobs "github.com/marvinrepremarca/seguros/internal/modules/renewals/adapters/jobs"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals/app"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/jobs"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

// Deps son las dependencias del módulo.
type Deps struct {
	DB      *db.DB
	Audit   *audit.Recorder
	Outbox  *outbox.Writer
	Parties parties.Directory
	Checker flags.Checker
	Policy  *authz.Policy
	Clock   kernel.Clock
	Logger  *slog.Logger
}

// Module expone las rutas.
type Module struct {
	server  *renewalshttp.Server
	checker flags.Checker
}

// New compone el módulo.
func New(d Deps) *Module {
	svc := &app.Service{Store: postgres.NewStore(d.DB, d.Audit, d.Outbox), Parties: d.Parties, Clock: d.Clock}
	return &Module{server: renewalshttp.NewServer(svc, d.Policy, d.Logger), checker: d.Checker}
}

// Mount registra las rutas detrás del flag: apagado, responde 404.
func (m *Module) Mount(r chi.Router) (err error) {
	r.Group(func(g chi.Router) {
		g.Use(flags.Require(m.checker, flags.RenewalsModule))
		err = m.server.Mount(g)
	})
	return err
}

// Consumers devuelve los consumidores de eventos (los registra cmd/worker).
func Consumers(database *db.DB, recorder *audit.Recorder, writer *outbox.Writer, clock kernel.Clock) []outbox.Consumer {
	return renewalsevents.Consumers(postgres.NewStore(database, recorder, writer), clock)
}

// RegisterJobs añade el job de alertas al worker (rol seguros_worker).
func RegisterJobs(workers *river.Workers, database *db.DB, recorder *audit.Recorder, writer *outbox.Writer, resolver jobs.FlagResolver, plans jobs.TenantPlans, clock kernel.Clock, logger *slog.Logger) []*river.PeriodicJob {
	store := postgres.NewStore(database, recorder, writer)
	return renewalsjobs.Register(workers, store, store, resolver, plans, clock, logger)
}
