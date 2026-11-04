// Package policies es el módulo núcleo de pólizas: máquina de estados, coberturas, bienes asegurados,
// endosos y cancelación (Flujos 3, 5 y 8). No es apagable y no depende de ningún módulo apagable: el
// control de SARLAFT se resuelve por flag (ADR 0012).
package policies

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/riverqueue/river"

	catalog "github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	identity "github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	policiesevents "github.com/marvinrepremarca/seguros/internal/modules/policies/adapters/events"
	policieshttp "github.com/marvinrepremarca/seguros/internal/modules/policies/adapters/http"
	policiesjobs "github.com/marvinrepremarca/seguros/internal/modules/policies/adapters/jobs"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/app"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

// Deps son las dependencias del módulo.
type Deps struct {
	DB      *db.DB
	Audit   *audit.Recorder
	Outbox  *outbox.Writer
	Parties parties.Directory
	Catalog catalog.Directory
	Schemas catalog.RiskSchemas
	Users   identity.Directory
	Flags   app.FlagResolver
	// Kyc es nil hasta que exista Compliance (Fase 5): usa app.NoKycRecords.
	Kyc    app.KycStatuses
	Policy *authz.Policy
	Clock  kernel.Clock
	Logger *slog.Logger
}

// Module expone las rutas.
type Module struct {
	server *policieshttp.Server
}

// New compone el módulo.
func New(d Deps) *Module {
	kyc := d.Kyc
	if kyc == nil {
		kyc = app.NoKycRecords{}
	}
	svc := &app.Service{Deps: app.Deps{
		Store: postgres.NewStore(d.DB, d.Audit, d.Outbox), Parties: d.Parties, Catalog: d.Catalog, Schemas: d.Schemas, Users: d.Users,
		Gate: &app.KycGate{Flags: d.Flags, Kyc: kyc, Logger: d.Logger}, Clock: d.Clock, Logger: d.Logger,
	}}
	return &Module{server: policieshttp.NewServer(svc, d.Policy, d.Logger)}
}

// Mount registra las rutas del módulo.
func (m *Module) Mount(r chi.Router) error { return m.server.Mount(r) }

// Consumers devuelve los consumidores de eventos del módulo (los registra cmd/worker).
func Consumers(database *db.DB, recorder *audit.Recorder, writer *outbox.Writer, clock kernel.Clock) []outbox.Consumer {
	return []outbox.Consumer{policiesevents.PortfolioTransfer(postgres.NewStore(database, recorder, writer), clock)}
}

// RegisterJobs añade el vencimiento diario de pólizas al worker (rol seguros_worker).
func RegisterJobs(workers *river.Workers, database *db.DB, recorder *audit.Recorder, writer *outbox.Writer, clock kernel.Clock) []*river.PeriodicJob {
	return policiesjobs.Register(workers, &app.Expirer{Store: postgres.NewStore(database, recorder, writer), Clock: clock})
}
