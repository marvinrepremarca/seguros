// Package billingjobs contiene los jobs de River de billing.
package billingjobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/marvinrepremarca/seguros/internal/modules/billing/app"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/jobs"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ScanArgs busca los tenants con cuotas por revisar.
type ScanArgs struct{}

// Kind implementa river.JobArgs.
func (ScanArgs) Kind() string { return "billing.overdue_scan" }

// TenantArgs revisa las cuotas de un tenant.
type TenantArgs struct {
	TenantID uuid.UUID `json:"tenantId"`
}

// Kind implementa river.JobArgs.
func (TenantArgs) Kind() string { return "billing.overdue_tenant" }

// Tenant implementa jobs.TenantArgs.
func (a TenantArgs) Tenant() kernel.TenantID { return kernel.TenantID(a.TenantID) }

// TenantLister lista los tenants con cuotas por revisar.
type TenantLister interface {
	TenantsWithDue(ctx context.Context, today kernel.Date) ([]kernel.TenantID, error)
}

// ScanWorker encola un job por tenant; el flag se evalúa en cada uno.
type ScanWorker struct {
	river.WorkerDefaults[ScanArgs]
	Tenants TenantLister
	Clock   kernel.Clock
}

// Work implementa river.Worker.
func (w *ScanWorker) Work(ctx context.Context, _ *river.Job[ScanArgs]) error {
	ids, err := w.Tenants.TenantsWithDue(ctx, kernel.Today(w.Clock))
	if err != nil {
		return err
	}
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("billing: cliente de jobs: %w", err)
	}
	for _, id := range ids {
		opts := &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Hour}}
		if _, err := client.Insert(ctx, TenantArgs{TenantID: uuid.UUID(id)}, opts); err != nil {
			return fmt.Errorf("billing: encolando revisión: %w", err)
		}
	}
	return nil
}

// Register añade los workers y el barrido periódico. Corre varias veces al día para que el cambio de
// fecha en America/Bogota no dependa de la hora de arranque; repetirlo no tiene efecto.
func Register(workers *river.Workers, store app.Store, lister TenantLister, resolver jobs.FlagResolver, plans jobs.TenantPlans, clock kernel.Clock, logger *slog.Logger) []*river.PeriodicJob {
	river.AddWorker(workers, &ScanWorker{Tenants: lister, Clock: clock})
	river.AddWorker[TenantArgs](workers, &jobs.TenantWorker[TenantArgs]{
		Flag: flags.BillingModule, Flags: resolver, Plans: plans, Logger: logger,
		Do: func(ctx context.Context, _ *river.Job[TenantArgs]) error { return app.MarkOverdue(ctx, store, clock) },
	})
	return []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(6*time.Hour), func() (river.JobArgs, *river.InsertOpts) { return ScanArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true, ID: "billing.overdue_scan"}),
	}
}
