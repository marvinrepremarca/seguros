package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// TenantArgs son los argumentos de un job de un tenant.
type TenantArgs interface {
	river.JobArgs
	Tenant() kernel.TenantID
}

// FlagResolver evalúa un flag para un tenant (lo implementa flags.Evaluator).
type FlagResolver interface {
	Resolve(ctx context.Context, tenant kernel.TenantID, plan string, key flags.Key) (flags.Resolution, error)
}

// TenantPlans resuelve el plan vigente de un tenant.
type TenantPlans interface {
	PlanOf(ctx context.Context, tenant kernel.TenantID) (string, error)
}

// TenantWorker ejecuta un job con el tenant de sus argumentos en el contexto y solo si el flag del
// módulo está encendido. Apagado, termina con éxito y sin efectos. Si el flag no se puede evaluar,
// devuelve error para que River reintente: el job no se da por hecho como si estuviera apagado.
type TenantWorker[T TenantArgs] struct {
	river.WorkerDefaults[T]
	Flag   flags.Key
	Flags  FlagResolver
	Plans  TenantPlans
	Logger *slog.Logger
	Do     func(ctx context.Context, job *river.Job[T]) error
}

// Work implementa river.Worker.
func (w *TenantWorker[T]) Work(ctx context.Context, job *river.Job[T]) error {
	tenant := job.Args.Tenant()
	if tenant == (kernel.TenantID{}) {
		return river.JobCancel(errors.New("jobs: job sin tenant"))
	}
	ctx = reqctx.WithTenantID(ctx, tenant)
	plan, err := w.Plans.PlanOf(ctx, tenant)
	if err != nil {
		return fmt.Errorf("jobs: plan del tenant: %w", err)
	}
	res, err := w.Flags.Resolve(ctx, tenant, plan, w.Flag)
	if err != nil {
		return fmt.Errorf("jobs: flag %s no evaluable: %w", w.Flag, err)
	}
	if res.Source == flags.SourceError {
		return fmt.Errorf("jobs: flag %s no evaluable", w.Flag)
	}
	if !res.Enabled {
		w.Logger.DebugContext(ctx, "job apagado por flag", "kind", job.Args.Kind(), "flag", string(w.Flag))
		return nil
	}
	return w.Do(ctx, job)
}
