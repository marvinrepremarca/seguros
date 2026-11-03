//go:build integration

package audit_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/pgtest"
)

//nolint:gochecknoglobals // compartidos por la suite (TestMain)
var (
	database *db.DB // rol seguros_app
	workerDB *db.DB // rol seguros_worker (job de particiones)
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
	database, err = env.OpenApp(ctx)
	if err != nil {
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer database.Close()
	workerDB, err = env.OpenWorker(ctx)
	if err != nil {
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer workerDB.Close()
	return m.Run()
}

func tenantCtx() context.Context {
	ctx := reqctx.WithTenantID(context.Background(), kernel.TenantID(uuid.Must(uuid.NewV7())))
	return reqctx.WithUserID(ctx, kernel.UserID(uuid.Must(uuid.NewV7())))
}

func entry() audit.Entry {
	return audit.Entry{
		Action: "identity.session.login", ResourceType: "identity.user", ResourceID: uuid.NewString(),
		Outcome: audit.OutcomeSuccess, Details: map[string]string{"method": "oidc"},
	}
}

func countEvents(ctx context.Context, t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit.events`).Scan(&n)
	}))
	return n
}

func TestRecordIsTenantIsolated(t *testing.T) {
	t.Parallel()
	rec := audit.NewRecorder(kernel.SystemClock{}) // la migración crea la partición del mes actual
	a, b := tenantCtx(), tenantCtx()

	require.NoError(t, database.WithTenantTx(a, func(tx pgx.Tx) error { return rec.Record(a, tx, entry()) }))

	assert.Equal(t, 1, countEvents(a, t))
	assert.Equal(t, 0, countEvents(b, t), "tenant B no ve eventos de A")

	var actorType string
	require.NoError(t, database.WithTenantTx(a, func(tx pgx.Tx) error {
		return tx.QueryRow(a, `SELECT actor_type FROM audit.events`).Scan(&actorType)
	}))
	assert.Equal(t, "user", actorType)
}

func TestRecordSystemActorAndDefaultPartition(t *testing.T) {
	t.Parallel()
	// Fecha lejana sin partición mensual: cae en la partición default y no se pierde.
	rec := audit.NewRecorder(kernel.NewFakeClock(time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)))
	ctx := reqctx.WithTenantID(context.Background(), kernel.TenantID(uuid.Must(uuid.NewV7())))
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error { return rec.Record(ctx, tx, entry()) }))

	var actorType string
	var actorID *uuid.UUID
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT actor_type, actor_id FROM audit.events`).Scan(&actorType, &actorID)
	}))
	assert.Equal(t, "system", actorType)
	assert.Nil(t, actorID)
}

func TestEventsAreAppendOnly(t *testing.T) {
	t.Parallel()
	rec := audit.NewRecorder(kernel.SystemClock{})
	ctx := tenantCtx()
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error { return rec.Record(ctx, tx, entry()) }))

	for _, stmt := range []string{
		`UPDATE audit.events SET outcome = 'failure'`,
		`DELETE FROM audit.events`,
		`TRUNCATE audit.events`,
	} {
		err := database.WithTenantTx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, stmt)
			return err
		})
		require.Error(t, err, stmt)
	}
	assert.Equal(t, 1, countEvents(ctx, t))
}

func TestCannotInsertForAnotherTenant(t *testing.T) {
	t.Parallel()
	ctx := tenantCtx()
	err := database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO audit.events (id, tenant_id, occurred_at, actor_type, action, resource_type, outcome)
			VALUES ($1, $2, now(), 'system', 'identity.session.login', 'identity.user', 'success')`,
			uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()))
		return err
	})
	require.Error(t, err, "RLS WITH CHECK rechaza otro tenant")
}

func TestRecordWithoutTenantFails(t *testing.T) {
	t.Parallel()
	rec := audit.NewRecorder(kernel.SystemClock{})
	err := database.WithoutTenantTx(context.Background(), func(tx pgx.Tx) error {
		return rec.Record(context.Background(), tx, entry())
	})
	require.ErrorIs(t, err, audit.ErrInvalidEntry)
}

func TestEnsurePartitionsIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var first, second int
	from, err := kernel.NewDate(2030, time.March, 1)
	require.NoError(t, err)
	call := func(d *db.DB, out *int) error {
		return d.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT audit.ensure_partitions($1, 2)`, from).Scan(out)
		})
	}
	require.NoError(t, call(workerDB, &first))
	require.NoError(t, call(workerDB, &second))
	assert.Equal(t, 3, first)
	assert.Equal(t, 0, second)
	require.Error(t, call(database, &first), "la API no crea particiones (solo el job del worker)")
}
