// Package documents es el módulo de gestión documental.
package documents

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/marvinrepremarca/seguros/internal/modules/documents/adapters/clamav"
	documentshttp "github.com/marvinrepremarca/seguros/internal/modules/documents/adapters/http"
	documentsjobs "github.com/marvinrepremarca/seguros/internal/modules/documents/adapters/jobs"
	"github.com/marvinrepremarca/seguros/internal/modules/documents/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/documents/adapters/s3"
	"github.com/marvinrepremarca/seguros/internal/modules/documents/adapters/unconfigured"
	"github.com/marvinrepremarca/seguros/internal/modules/documents/app"
	"github.com/marvinrepremarca/seguros/internal/modules/documents/domain"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
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

// Unconfigured reemplaza almacenamiento y antivirus si el entorno no los configuró.
func Unconfigured() unconfigured.Storage { return unconfigured.Storage{} }

// NewScanner construye el cliente de clamd.
func NewScanner(addr string) app.Scanner { return clamav.Scanner{Addr: addr} }

// Owners valida el dueño de un documento con las consultas públicas de parties y policies. Claims es
// apagable y no publica consulta: un siniestro se acepta sin validar (ROADMAP).
type Owners struct {
	Parties  parties.Directory
	Policies policies.Directory
}

// Exists implementa app.Owners.
func (o Owners) Exists(ctx context.Context, t domain.OwnerType, id uuid.UUID) (bool, error) {
	switch t {
	case domain.OwnerParty:
		found, err := o.Parties.Summaries(ctx, []uuid.UUID{id})
		_, ok := found[id]
		return ok, err
	case domain.OwnerPolicy:
		_, ok, err := o.Policies.Facts(ctx, id)
		return ok, err
	case domain.OwnerClaim:
		return true, nil
	default:
		return false, nil
	}
}

// Deps son las dependencias del módulo.
type Deps struct {
	DB            *db.DB
	Audit         *audit.Recorder
	Storage       app.Storage
	Scanner       app.Scanner
	Cipher        app.Cipher
	Owners        app.Owners
	Checker       flags.Checker
	Policy        *authz.Policy
	Clock         kernel.Clock
	Logger        *slog.Logger
	MaxBytes      int64
	RetentionDays int32
	DownloadTTL   time.Duration
}

func service(d Deps) *app.Service {
	return &app.Service{Store: postgres.NewStore(d.DB, d.Audit), Storage: d.Storage, Scanner: d.Scanner, Cipher: d.Cipher, Owners: d.Owners, Clock: d.Clock, Logger: d.Logger, MaxBytes: d.MaxBytes, RetentionDays: d.RetentionDays, DownloadTTL: d.DownloadTTL}
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
