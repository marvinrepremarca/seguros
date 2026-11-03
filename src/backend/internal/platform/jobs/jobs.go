// Package jobs envuelve River (edición OSS, MPL-2.0) para los trabajos programados y en segundo
// plano (ARCHITECTURE.md §5): cliente del worker con jobs periódicos, cliente de solo inserción
// para la API y InsertTx para encolar dentro de la misma transacción del cambio.
//
// Los argumentos de un job llevan IDs y el tenant (nunca datos Restringidos); el worker abre
// db.WithTenantTx con ese tenant y verifica el flag del módulo antes de actuar
// (rules/feature-flags.md §2).
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/db/migrate"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Client es el cliente River sobre pgx.
type Client = river.Client[pgx.Tx]

// Inserter encola jobs dentro de una transacción existente (pgx.Tx de db.WithTenantTx).
type Inserter interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) error
}

type inserter struct{ client *Client }

func (i inserter) InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) error {
	if _, err := i.client.InsertTx(ctx, tx, args, opts); err != nil {
		return fmt.Errorf("jobs: encolando %s: %w", args.Kind(), err)
	}
	return nil
}

// NewInsertOnlyClient crea un cliente que solo encola (cmd/api: no ejecuta jobs).
func NewInsertOnlyClient(pool *pgxpool.Pool, logger *slog.Logger) (Inserter, error) {
	c, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Schema: migrate.RiverSchema, Logger: logger})
	if err != nil {
		return nil, fmt.Errorf("jobs: cliente de inserción: %w", err)
	}
	return inserter{client: c}, nil
}

// WorkerOptions configura el cliente del worker.
type WorkerOptions struct {
	Workers  *river.Workers
	Periodic []*river.PeriodicJob
	// MaxWorkers de la cola por defecto (default 10).
	MaxWorkers int
	Logger     *slog.Logger
}

// NewWorkerClient crea el cliente que ejecuta jobs (cmd/worker). Se arranca con Start(ctx) y se
// detiene con Stop(ctx).
func NewWorkerClient(pool *pgxpool.Pool, opts WorkerOptions) (*Client, Inserter, error) {
	if opts.MaxWorkers <= 0 {
		opts.MaxWorkers = 10
	}
	c, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Schema:       migrate.RiverSchema,
		Logger:       opts.Logger,
		Queues:       map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: opts.MaxWorkers}},
		Workers:      opts.Workers,
		PeriodicJobs: opts.Periodic,
		JobTimeout:   5 * time.Minute,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("jobs: cliente del worker: %w", err)
	}
	return c, inserter{client: c}, nil
}

// --- Jobs periódicos de plataforma ---

// AuditPartitionsArgs crea por adelantado las particiones mensuales de audit.events.
type AuditPartitionsArgs struct{}

// Kind implementa river.JobArgs.
func (AuditPartitionsArgs) Kind() string { return "platform.audit_partitions" }

// AuditPartitionsWorker ejecuta audit.ensure_partitions (SECURITY DEFINER) desde el mes actual
// hasta 3 meses después.
type AuditPartitionsWorker struct {
	river.WorkerDefaults[AuditPartitionsArgs]
	DB    *db.DB
	Clock kernel.Clock
}

// Work implementa river.Worker.
func (w *AuditPartitionsWorker) Work(ctx context.Context, _ *river.Job[AuditPartitionsArgs]) error {
	today := kernel.Today(w.Clock)
	return w.DB.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		var created int
		if err := tx.QueryRow(ctx, `SELECT audit.ensure_partitions($1, 3)`, today).Scan(&created); err != nil {
			return fmt.Errorf("jobs: particiones de auditoría: %w", err)
		}
		return nil
	})
}

// OutboxPurgeArgs borra eventos ya publicados con más de 7 días.
type OutboxPurgeArgs struct{}

// Kind implementa river.JobArgs.
func (OutboxPurgeArgs) Kind() string { return "platform.outbox_purge" }

// OutboxPurgeWorker ejecuta messaging.purge_published (SECURITY DEFINER).
type OutboxPurgeWorker struct {
	river.WorkerDefaults[OutboxPurgeArgs]
	DB    *db.DB
	Clock kernel.Clock
}

// Work implementa river.Worker.
func (w *OutboxPurgeWorker) Work(ctx context.Context, _ *river.Job[OutboxPurgeArgs]) error {
	before := w.Clock.Now().Add(-7 * 24 * time.Hour)
	return w.DB.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT messaging.purge_published($1)`, before).Scan(&n); err != nil {
			return fmt.Errorf("jobs: limpiando outbox: %w", err)
		}
		return nil
	})
}

// PlatformJobs registra los workers y jobs periódicos de plataforma (diarios; también al arrancar).
func PlatformJobs(workers *river.Workers, database *db.DB, clock kernel.Clock) []*river.PeriodicJob {
	river.AddWorker(workers, &AuditPartitionsWorker{DB: database, Clock: clock})
	river.AddWorker(workers, &OutboxPurgeWorker{DB: database, Clock: clock})
	daily := river.PeriodicInterval(24 * time.Hour)
	return []*river.PeriodicJob{
		river.NewPeriodicJob(daily, func() (river.JobArgs, *river.InsertOpts) { return AuditPartitionsArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true, ID: "platform.audit_partitions"}),
		river.NewPeriodicJob(daily, func() (river.JobArgs, *river.InsertOpts) { return OutboxPurgeArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true, ID: "platform.outbox_purge"}),
	}
}
