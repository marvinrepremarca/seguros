// Package policiesjobs contiene los jobs de River de policies.
package policiesjobs

import (
	"context"
	"fmt"
	"time"

	"github.com/riverqueue/river"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/app"
)

// ExpireArgs dispara el vencimiento diario de pólizas.
type ExpireArgs struct{}

// Kind implementa river.JobArgs.
func (ExpireArgs) Kind() string { return "policies.expire_due" }

// ExpireWorker ejecuta app.Expirer.
type ExpireWorker struct {
	river.WorkerDefaults[ExpireArgs]
	Expirer *app.Expirer
}

// Work implementa river.Worker.
func (w *ExpireWorker) Work(ctx context.Context, _ *river.Job[ExpireArgs]) error {
	if _, err := w.Expirer.Run(ctx); err != nil {
		return fmt.Errorf("policies: venciendo pólizas: %w", err)
	}
	return nil
}

// Register añade el worker y su ejecución periódica. Corre más de una vez al día para que el cambio de
// fecha en America/Bogota no dependa de la hora de arranque; repetirlo no tiene efecto.
func Register(workers *river.Workers, expirer *app.Expirer) []*river.PeriodicJob {
	river.AddWorker(workers, &ExpireWorker{Expirer: expirer})
	return []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(6*time.Hour), func() (river.JobArgs, *river.InsertOpts) { return ExpireArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true, ID: "policies.expire_due"}),
	}
}
