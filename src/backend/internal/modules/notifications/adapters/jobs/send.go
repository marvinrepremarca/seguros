// Package notificationsjobs contiene los jobs de River de notificaciones.
package notificationsjobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/marvinrepremarca/seguros/internal/modules/notifications/app"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/jobs"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ScanArgs busca los tenants con avisos por enviar.
type ScanArgs struct{}

// Kind implementa river.JobArgs.
func (ScanArgs) Kind() string { return "notifications.send_sweep" }

// TenantArgs envía los avisos de un tenant.
type TenantArgs struct {
	TenantID uuid.UUID `json:"tenantId"`
}

// Kind implementa river.JobArgs.
func (TenantArgs) Kind() string { return "notifications.send_tenant" }

// Tenant implementa jobs.TenantArgs.
func (a TenantArgs) Tenant() kernel.TenantID { return kernel.TenantID(a.TenantID) }

// TenantLister lista los tenants con avisos por enviar.
type TenantLister interface {
	TenantsWithPendingMessages(ctx context.Context) ([]kernel.TenantID, error)
}

// ScanWorker encola un job por tenant; el flag se evalúa en cada uno.
type ScanWorker struct {
	river.WorkerDefaults[ScanArgs]
	Tenants TenantLister
}

// Work implementa river.Worker.
func (w *ScanWorker) Work(ctx context.Context, _ *river.Job[ScanArgs]) error {
	ids, err := w.Tenants.TenantsWithPendingMessages(ctx)
	if err != nil {
		return err
	}
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("notifications: cliente de jobs: %w", err)
	}
	for _, id := range ids {
		opts := &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Minute}}
		if _, err := client.Insert(ctx, TenantArgs{TenantID: uuid.UUID(id)}, opts); err != nil {
			return fmt.Errorf("notifications: encolando envío: %w", err)
		}
	}
	return nil
}

// Register añade los workers y el barrido periódico.
func Register(workers *river.Workers, svc *app.Service, lister TenantLister, resolver jobs.FlagResolver, plans jobs.TenantPlans, logger *slog.Logger) []*river.PeriodicJob {
	river.AddWorker(workers, &ScanWorker{Tenants: lister})
	river.AddWorker[TenantArgs](workers, &jobs.TenantWorker[TenantArgs]{
		Flag: flags.NotificationsModule, Flags: resolver, Plans: plans, Logger: logger,
		Do: func(ctx context.Context, _ *river.Job[TenantArgs]) error { return svc.SendPending(ctx) },
	})
	return []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(time.Minute), func() (river.JobArgs, *river.InsertOpts) { return ScanArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true, ID: "notifications.send_sweep"}),
	}
}
