// Package jobs envuelve River (edición OSS, MPL-2.0) para los trabajos programados y en segundo
// plano (ARCHITECTURE.md §5): cliente del worker con jobs periódicos e InsertTx para encolar dentro
// de una transacción. Solo el rol seguros_worker accede a las tablas de River (no tienen RLS): un
// módulo que necesita un job desde la API publica un evento (outbox, atado por RLS a su tenant) y
// su consumidor en el worker lo encola con el tenant del evento.
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
	"go.opentelemetry.io/otel"

	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/db/migrate"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
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

// InboxPurgeArgs borra marcas de procesado con más de 30 días. Supera con holgura los 7 días del
// outbox y las reentregas; un reproceso solo llega a quien falló, y ese no dejó marca.
type InboxPurgeArgs struct{}

// Kind implementa river.JobArgs.
func (InboxPurgeArgs) Kind() string { return "platform.inbox_purge" }

// InboxPurgeWorker ejecuta messaging.purge_inbox (SECURITY DEFINER).
type InboxPurgeWorker struct {
	river.WorkerDefaults[InboxPurgeArgs]
	DB    *db.DB
	Clock kernel.Clock
}

// Work implementa river.Worker.
func (w *InboxPurgeWorker) Work(ctx context.Context, _ *river.Job[InboxPurgeArgs]) error {
	before := w.Clock.Now().Add(-30 * 24 * time.Hour)
	return w.DB.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT messaging.purge_inbox($1)`, before).Scan(&n); err != nil {
			return fmt.Errorf("jobs: limpiando inbox: %w", err)
		}
		return nil
	})
}

// outboxDelayWarning es el retraso a partir del cual el relay se considera atrasado.
const outboxDelayWarning = 5 * time.Minute

// MessagingHealthArgs mide la salud de la mensajería.
type MessagingHealthArgs struct{}

// Kind implementa river.JobArgs.
func (MessagingHealthArgs) Kind() string { return "platform.messaging_health" }

// MessagingHealthWorker publica las métricas sobre las que se definen las alertas y deja un aviso en
// el log cuando hay mensajes fallidos o retraso.
type MessagingHealthWorker struct {
	river.WorkerDefaults[MessagingHealthArgs]
	DB     *db.DB
	Clock  kernel.Clock
	Logger *slog.Logger
}

// Work implementa river.Worker.
func (w *MessagingHealthWorker) Work(ctx context.Context, _ *river.Job[MessagingHealthArgs]) error {
	h, err := outbox.ReadHealth(ctx, w.DB, w.Clock.Now())
	if err != nil {
		return err
	}
	meter := otel.Meter("seguros/messaging")
	gauges := []struct {
		name  string
		value int64
	}{
		{"messaging.dead_letters.pending", h.DeadLettersPending},
		{"messaging.outbox.stuck", h.Stuck},
		{"messaging.outbox.oldest_pending_seconds", int64(h.OldestPending / time.Second)},
	}
	for _, g := range gauges {
		gauge, err := meter.Int64Gauge(g.name)
		if err != nil {
			return fmt.Errorf("jobs: métrica %s: %w", g.name, err)
		}
		gauge.Record(ctx, g.value)
	}
	if h.DeadLettersPending > 0 || h.Stuck > 0 || h.OldestPending > outboxDelayWarning {
		w.Logger.WarnContext(ctx, "mensajería con atención pendiente",
			"dead_letters", h.DeadLettersPending, "stuck", h.Stuck, "oldest_pending_seconds", int64(h.OldestPending/time.Second))
	}
	return nil
}

// PlatformJobs registra los workers y jobs periódicos de plataforma (también al arrancar).
func PlatformJobs(workers *river.Workers, database *db.DB, clock kernel.Clock, logger *slog.Logger) []*river.PeriodicJob {
	river.AddWorker(workers, &AuditPartitionsWorker{DB: database, Clock: clock})
	river.AddWorker(workers, &OutboxPurgeWorker{DB: database, Clock: clock})
	river.AddWorker(workers, &InboxPurgeWorker{DB: database, Clock: clock})
	river.AddWorker(workers, &MessagingHealthWorker{DB: database, Clock: clock, Logger: logger})
	daily := river.PeriodicInterval(24 * time.Hour)
	return []*river.PeriodicJob{
		river.NewPeriodicJob(daily, func() (river.JobArgs, *river.InsertOpts) { return InboxPurgeArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true, ID: "platform.inbox_purge"}),
		river.NewPeriodicJob(river.PeriodicInterval(time.Minute), func() (river.JobArgs, *river.InsertOpts) { return MessagingHealthArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true, ID: "platform.messaging_health"}),
		river.NewPeriodicJob(daily, func() (river.JobArgs, *river.InsertOpts) { return AuditPartitionsArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true, ID: "platform.audit_partitions"}),
		river.NewPeriodicJob(daily, func() (river.JobArgs, *river.InsertOpts) { return OutboxPurgeArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true, ID: "platform.outbox_purge"}),
	}
}
