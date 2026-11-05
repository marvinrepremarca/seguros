//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/jobs"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/pgtest"
)

//nolint:gochecknoglobals // compartidos por la suite (TestMain)
var (
	database *db.DB // rol seguros_worker
	appDB    *db.DB // rol seguros_app
)

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	ctx := context.Background()
	env, err := pgtest.Start(ctx)
	if err != nil {
		slog.Error("iniciando PostgreSQL", "error", err)
		return 1
	}
	defer env.Terminate() //nolint:errcheck // limpieza al final de la suite
	if err := env.MigrateAll(ctx); err != nil {
		slog.Error("migrando", "error", err)
		return 1
	}
	database, err = env.OpenWorker(ctx)
	if err != nil {
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer database.Close()
	appDB, err = env.OpenApp(ctx)
	if err != nil {
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer appDB.Close()
	return m.Run()
}

// pingArgs es un job de prueba con el tenant en sus argumentos.
type pingArgs struct {
	TenantID uuid.UUID `json:"tenantId"`
}

func (pingArgs) Kind() string { return "test.ping" }

type pingWorker struct {
	river.WorkerDefaults[pingArgs]
	done chan uuid.UUID
}

func (w *pingWorker) Work(_ context.Context, j *river.Job[pingArgs]) error {
	w.done <- j.Args.TenantID
	return nil
}

func TestInsertTxIsTransactionalAndJobsRun(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	worker := &pingWorker{done: make(chan uuid.UUID, 4)}
	workers := river.NewWorkers()
	river.AddWorker(workers, worker)
	periodic := jobs.PlatformJobs(workers, database, kernel.SystemClock{}, logger)
	client, inserter, err := jobs.NewWorkerClient(database.Pool(), jobs.WorkerOptions{Workers: workers, Periodic: periodic, Logger: logger})
	require.NoError(t, err)

	tenant := uuid.Must(uuid.NewV7())
	ctx := reqctx.WithTenantID(context.Background(), kernel.TenantID(tenant))

	// Encolado dentro de la transacción del cambio: si hay rollback, el job no existe.
	err = database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		if err := inserter.InsertTx(ctx, tx, pingArgs{TenantID: uuid.Must(uuid.NewV7())}, nil); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	require.Error(t, err)
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return inserter.InsertTx(ctx, tx, pingArgs{TenantID: tenant}, nil)
	}))

	runCtx, cancel := context.WithCancel(context.Background())
	require.NoError(t, client.Start(runCtx))
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		require.NoError(t, client.Stop(stopCtx))
		cancel()
	})

	select {
	case got := <-worker.done:
		assert.Equal(t, tenant, got, "solo corre el job confirmado")
	case <-time.After(15 * time.Second):
		t.Fatal("el job no se ejecutó")
	}
	select {
	case got := <-worker.done:
		t.Fatalf("se ejecutó un job de una transacción revertida: %s", got)
	case <-time.After(500 * time.Millisecond):
	}

	// Los jobs periódicos de plataforma corren al arrancar y terminan sin error.
	require.Eventually(t, func() bool {
		var n int
		err := database.WithoutTenantTx(context.Background(), func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(),
				`SELECT count(*) FROM river.river_job WHERE kind IN ('platform.audit_partitions', 'platform.outbox_purge', 'platform.inbox_purge', 'platform.messaging_health') AND state = 'completed'`).Scan(&n)
		})
		return err == nil && n == 4
	}, 15*time.Second, 100*time.Millisecond)
}

func TestAPIRoleHasNoRiverAccess(t *testing.T) {
	for _, q := range []string{
		`SELECT count(*) FROM river.river_job`,
		`INSERT INTO river.river_job (args, kind, max_attempts, queue, state) VALUES ('{}', 'test.x', 1, 'default', 'available')`,
	} {
		err := appDB.WithoutTenantTx(context.Background(), func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), q)
			return err
		})
		require.Error(t, err, "la API no toca River (los jobs los encola el worker): %s", q)
	}
}
