// Package billing es el módulo apagable de cartera y cobro (Flujo 4). Reacciona a los eventos de
// policies; apagarlo no afecta la expedición ni borra datos.
package billing

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/riverqueue/river"

	billingevents "github.com/marvinrepremarca/seguros/internal/modules/billing/adapters/events"
	billinghttp "github.com/marvinrepremarca/seguros/internal/modules/billing/adapters/http"
	billingjobs "github.com/marvinrepremarca/seguros/internal/modules/billing/adapters/jobs"
	"github.com/marvinrepremarca/seguros/internal/modules/billing/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/billing/app"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
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
	server  *billinghttp.Server
	checker flags.Checker
}

// New compone el módulo.
func New(d Deps) *Module {
	svc := &app.Service{Store: postgres.NewStore(d.DB, d.Audit, d.Outbox, d.Clock), Parties: d.Parties, Clock: d.Clock}
	return &Module{server: billinghttp.NewServer(svc, d.Policy, d.Logger), checker: d.Checker}
}

// Mount registra las rutas detrás del flag: apagado, responde 404.
func (m *Module) Mount(r chi.Router) (err error) {
	r.Group(func(g chi.Router) {
		g.Use(flags.Require(m.checker, flags.BillingModule))
		err = m.server.Mount(g)
	})
	return err
}

// Consumers devuelve los consumidores de eventos (los registra cmd/worker).
func Consumers(database *db.DB, recorder *audit.Recorder, writer *outbox.Writer, clock kernel.Clock) []outbox.Consumer {
	return billingevents.Consumers(postgres.NewStore(database, recorder, writer, clock), clock)
}

// RegisterJobs añade el job de mora al worker (rol seguros_worker).
func RegisterJobs(workers *river.Workers, database *db.DB, recorder *audit.Recorder, writer *outbox.Writer, resolver jobs.FlagResolver, plans jobs.TenantPlans, clock kernel.Clock, logger *slog.Logger) []*river.PeriodicJob {
	store := postgres.NewStore(database, recorder, writer, clock)
	return billingjobs.Register(workers, store, store, resolver, plans, clock, logger)
}
