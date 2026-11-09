// Package notifications es el módulo de avisos a clientes.
package notifications

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/riverqueue/river"

	notificationsevents "github.com/marvinrepremarca/seguros/internal/modules/notifications/adapters/events"
	notificationshttp "github.com/marvinrepremarca/seguros/internal/modules/notifications/adapters/http"
	notificationsjobs "github.com/marvinrepremarca/seguros/internal/modules/notifications/adapters/jobs"
	"github.com/marvinrepremarca/seguros/internal/modules/notifications/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/notifications/adapters/smtp"
	"github.com/marvinrepremarca/seguros/internal/modules/notifications/app"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/jobs"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

// SMTPConfig son los datos del servidor de correo.
type SMTPConfig = smtp.Config

// NewSMTPSender construye el canal de correo.
func NewSMTPSender(c SMTPConfig) app.Sender { return smtp.Sender{C: c} }

// Deps son las dependencias del módulo.
type Deps struct {
	DB       *db.DB
	Audit    *audit.Recorder
	Sender   app.Sender
	Policies policies.Directory
	Parties  parties.ContactView
	Checker  flags.Checker
	Policy   *authz.Policy
	Clock    kernel.Clock
	Logger   *slog.Logger
}

func service(d Deps) *app.Service {
	return &app.Service{Store: postgres.NewStore(d.DB, d.Audit), Sender: d.Sender, Policies: d.Policies, Parties: d.Parties, Clock: d.Clock, Logger: d.Logger}
}

// Module expone la bandeja de salida.
type Module struct {
	server  *notificationshttp.Server
	checker flags.Checker
	// Service queda expuesto para las pruebas del envío.
	Service *app.Service
}

// New compone el módulo.
func New(d Deps) *Module {
	svc := service(d)
	return &Module{server: notificationshttp.NewServer(svc, d.Policy, d.Logger), checker: d.Checker, Service: svc}
}

// Mount registra las rutas detrás del flag: apagado, responde 404.
func (m *Module) Mount(r chi.Router) (err error) {
	r.Group(func(g chi.Router) {
		g.Use(flags.Require(m.checker, flags.NotificationsModule))
		err = m.server.Mount(g)
	})
	return err
}

// Consumers devuelve los consumidores de eventos.
func Consumers(database *db.DB, recorder *audit.Recorder, clock kernel.Clock) []outbox.Consumer {
	return notificationsevents.Consumers(postgres.NewStore(database, recorder), clock)
}

// RegisterJobs añade el envío al worker.
func RegisterJobs(workers *river.Workers, d Deps, resolver jobs.FlagResolver, plans jobs.TenantPlans) []*river.PeriodicJob {
	return notificationsjobs.Register(workers, service(d), postgres.NewStore(d.DB, d.Audit), resolver, plans, d.Logger)
}
