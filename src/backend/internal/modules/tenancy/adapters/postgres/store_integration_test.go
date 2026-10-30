//go:build integration

package postgres_test

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

	"github.com/marvinrepremarca/seguros/internal/modules/tenancy"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/app"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/pgtest"
)

var database *db.DB //nolint:gochecknoglobals // compartido por la suite (TestMain)

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
	return m.Run()
}

func newModule() *tenancy.Module {
	return tenancy.New(database, kernel.NewFakeClock(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)))
}

func uniqueAlias() string { return "ag-" + uuid.Must(uuid.NewV7()).String()[24:] }

func TestProvisionResolveAndUpdate(t *testing.T) {
	t.Parallel()
	mod := newModule()
	ctx := context.Background()
	alias := uniqueAlias()

	id, err := mod.Provision.Handle(ctx, app.ProvisionTenantCommand{Name: "Agencia", Plan: "basico", OrgAlias: alias})
	require.NoError(t, err)

	info, err := mod.Directory.ResolveByOrgAlias(ctx, alias)
	require.NoError(t, err)
	assert.Equal(t, contracts.TenantInfo{ID: id, Plan: "basico", Active: true}, info)

	require.NoError(t, mod.ChangeStatus.Handle(ctx, app.ChangeTenantStatusCommand{OrgAlias: alias, Suspend: true}))
	require.NoError(t, mod.ChangePlan.Handle(ctx, app.ChangeTenantPlanCommand{OrgAlias: alias, Plan: "empresarial"}))
	info, err = mod.Directory.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, contracts.TenantInfo{ID: id, Plan: "empresarial", Active: false}, info)

	_, err = mod.Directory.ResolveByOrgAlias(ctx, "no-vinculada")
	require.ErrorIs(t, err, contracts.ErrTenantNotFound)

	_, err = mod.Provision.Handle(ctx, app.ProvisionTenantCommand{Name: "Otra", Plan: "basico", OrgAlias: alias})
	require.ErrorIs(t, err, domain.ErrOrgAliasTaken)
}

// Un tenant no ve la fila de otro aunque conozca su ID.
func TestTenantIsolation(t *testing.T) {
	t.Parallel()
	mod := newModule()
	ctx := context.Background()
	idA, err := mod.Provision.Handle(ctx, app.ProvisionTenantCommand{Name: "A", Plan: "basico", OrgAlias: uniqueAlias()})
	require.NoError(t, err)
	idB, err := mod.Provision.Handle(ctx, app.ProvisionTenantCommand{Name: "B", Plan: "basico", OrgAlias: uniqueAlias()})
	require.NoError(t, err)

	ctxB := reqctx.WithTenantID(ctx, idB)
	var n int
	require.NoError(t, database.WithTenantTx(ctxB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctxB, `SELECT count(*) FROM tenancy.tenants WHERE id = $1`, uuid.UUID(idA)).Scan(&n)
	}))
	assert.Zero(t, n, "B no ve a A")
	require.NoError(t, database.WithTenantTx(ctxB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctxB, `SELECT count(*) FROM tenancy.tenants`).Scan(&n)
	}))
	assert.Equal(t, 1, n, "B solo se ve a sí mismo")

	var updated int64
	require.NoError(t, database.WithTenantTx(ctxB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctxB, `UPDATE tenancy.tenants SET status = 'suspended' WHERE id = $1`, uuid.UUID(idA))
		updated = tag.RowsAffected()
		return err
	}))
	assert.Zero(t, updated)
}

// La app no puede leer el registro completo ni borrar tenants; la función de resolución expone
// solo id y estado.
func TestAppRoleLimits(t *testing.T) {
	t.Parallel()
	mod := newModule()
	ctx := context.Background()
	id, err := mod.Provision.Handle(ctx, app.ProvisionTenantCommand{Name: "A", Plan: "basico", OrgAlias: uniqueAlias()})
	require.NoError(t, err)
	ctxA := reqctx.WithTenantID(ctx, id)

	err = database.WithTenantTx(ctxA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctxA, `DELETE FROM tenancy.tenants WHERE id = $1`, uuid.UUID(id))
		return err
	})
	require.ErrorContains(t, err, "permission denied")

	err = database.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		var n int
		return tx.QueryRow(ctx, `SELECT count(*) FROM tenancy.tenants`).Scan(&n)
	})
	require.Error(t, err, "sin tenant no hay lectura directa")
}
