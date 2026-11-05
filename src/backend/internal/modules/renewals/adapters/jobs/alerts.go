// Package renewalsjobs contiene los jobs de River de renovaciones.
package renewalsjobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/marvinrepremarca/seguros/internal/modules/renewals/app"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/jobs"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// scanWindowDays cubre el mayor umbral configurable (domain.ValidateAlertDays).
const scanWindowDays = 365

// ScanArgs busca los tenants con casos por alertar.
type ScanArgs struct{}

// Kind implementa river.JobArgs.
func (ScanArgs) Kind() string { return "renewals.alert_scan" }

// TenantArgs envía las alertas de un tenant.
type TenantArgs struct {
	TenantID uuid.UUID `json:"tenantId"`
}

// Kind implementa river.JobArgs.
func (TenantArgs) Kind() string { return "renewals.alert_tenant" }

// Tenant implementa jobs.TenantArgs.
func (a TenantArgs) Tenant() kernel.TenantID { return kernel.TenantID(a.TenantID) }

// TenantLister lista los tenants con casos por alertar.
type TenantLister interface {
	TenantsWithOpenCases(ctx context.Context, until kernel.Date) ([]kernel.TenantID, error)
}

// ScanWorker encola un job por tenant; el flag se evalúa en cada uno.
type ScanWorker struct {
	river.WorkerDefaults[ScanArgs]
	Tenants TenantLister
	Clock   kernel.Clock
}

// Work implementa river.Worker.
func (w *ScanWorker) Work(ctx context.Context, _ *river.Job[ScanArgs]) error {
	ids, err := w.Tenants.TenantsWithOpenCases(ctx, kernel.Today(w.Clock).AddDays(scanWindowDays))
	if err != nil {
		return err
	}
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("renewals: cliente de jobs: %w", err)
	}
	for _, id := range ids {
		opts := &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Hour}}
		if _, err := client.Insert(ctx, TenantArgs{TenantID: uuid.UUID(id)}, opts); err != nil {
			return fmt.Errorf("renewals: encolando alertas: %w", err)
		}
	}
	return nil
}

// Register añade los workers y el barrido periódico.
func Register(workers *river.Workers, store app.Store, lister TenantLister, resolver jobs.FlagResolver, plans jobs.TenantPlans, clock kernel.Clock, logger *slog.Logger) []*river.PeriodicJob {
	river.AddWorker(workers, &ScanWorker{Tenants: lister, Clock: clock})
	river.AddWorker[TenantArgs](workers, &jobs.TenantWorker[TenantArgs]{
		Flag: flags.RenewalsModule, Flags: resolver, Plans: plans, Logger: logger,
		Do: func(ctx context.Context, _ *river.Job[TenantArgs]) error { return app.SendAlerts(ctx, store, clock) },
	})
	return []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(6*time.Hour), func() (river.JobArgs, *river.InsertOpts) { return ScanArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true, ID: "renewals.alert_scan"}),
	}
}
