// Package documents es el módulo de gestión documental.
package documents

import (
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riverqueue/river"

	"github.com/marvinrepremarca/seguros/internal/modules/documents/adapters/clamav"
	documentshttp "github.com/marvinrepremarca/seguros/internal/modules/documents/adapters/http"
	documentsjobs "github.com/marvinrepremarca/seguros/internal/modules/documents/adapters/jobs"
	"github.com/marvinrepremarca/seguros/internal/modules/documents/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/documents/adapters/s3"
	"github.com/marvinrepremarca/seguros/internal/modules/documents/app"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/jobs"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// S3Config son los datos de conexión al almacenamiento.
type S3Config = s3.Config

// NewStorage construye el almacenamiento S3-compatible.
func NewStorage(c S3Config) (*s3.Storage, error) { return s3.New(c) }

// NewScanner construye el cliente de clamd.
func NewScanner(addr string) app.Scanner { return clamav.Scanner{Addr: addr} }

// Deps son las dependencias del módulo.
type Deps struct {
	DB            *db.DB
	Audit         *audit.Recorder
	Storage       app.Storage
	Scanner       app.Scanner
	Cipher        app.Cipher
	Checker       flags.Checker
	Policy        *authz.Policy
	Clock         kernel.Clock
	Logger        *slog.Logger
	MaxBytes      int64
	RetentionDays int32
	DownloadTTL   time.Duration
}

func service(d Deps) *app.Service {
	return &app.Service{Store: postgres.NewStore(d.DB, d.Audit), Storage: d.Storage, Scanner: d.Scanner, Cipher: d.Cipher, Clock: d.Clock, Logger: d.Logger, MaxBytes: d.MaxBytes, RetentionDays: d.RetentionDays, DownloadTTL: d.DownloadTTL}
}

// Module expone las rutas.
type Module struct {
	server  *documentshttp.Server
	checker flags.Checker
	// Service queda expuesto para que el worker y las pruebas ejecuten la revisión antivirus.
	Service *app.Service
}

// New compone el módulo.
func New(d Deps) *Module {
	svc := service(d)
	return &Module{server: documentshttp.NewServer(svc, d.Policy, d.Logger), checker: d.Checker, Service: svc}
}

// Mount registra las rutas detrás del flag: apagado, responde 404.
func (m *Module) Mount(r chi.Router) (err error) {
	r.Group(func(g chi.Router) {
		g.Use(flags.Require(m.checker, flags.DocumentsModule))
		err = m.server.Mount(g)
	})
	return err
}

// RegisterJobs añade la revisión antivirus al worker.
func RegisterJobs(workers *river.Workers, d Deps, resolver jobs.FlagResolver, plans jobs.TenantPlans) []*river.PeriodicJob {
	return documentsjobs.Register(workers, service(d), postgres.NewStore(d.DB, d.Audit), resolver, plans, d.Logger)
}
