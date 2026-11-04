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

//nolint:gochecknoglobals // compartidos por la suite (TestMain)
var (
	database *db.DB    // rol seguros_platform
	appDB    *db.DB    // rol seguros_app (API)
	pgSuper  *pgx.Conn // superusuario: solo para verificar messaging.outbox (la app no lo lee)
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
	database, err = env.OpenPlatform(ctx) // alta/plan/suspensión: solo platformctl
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
	pgSuper, err = pgx.Connect(ctx, env.SuperuserURL)
	if err != nil {
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer pgSuper.Close(ctx) //nolint:errcheck // limpieza al final de la suite
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

	id, err := mod.Provision.Handle(ctx, app.ProvisionTenantCommand{Reason: "prueba", Operator: "ops.test", Name: "Agencia", Plan: "basico", OrgAlias: alias})
	require.NoError(t, err)

	info, err := mod.Directory.ResolveByOrgAlias(ctx, alias)
	require.NoError(t, err)
	assert.Equal(t, contracts.TenantInfo{ID: id, Plan: "basico", Active: true}, info)

	require.NoError(t, mod.ChangeStatus.Handle(ctx, app.ChangeTenantStatusCommand{Reason: "prueba", Operator: "ops.test", OrgAlias: alias, Suspend: true}))
	require.NoError(t, mod.ChangePlan.Handle(ctx, app.ChangeTenantPlanCommand{Reason: "prueba", Operator: "ops.test", OrgAlias: alias, Plan: "empresarial"}))
	info, err = mod.Directory.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, contracts.TenantInfo{ID: id, Plan: "empresarial", Active: false}, info)

	_, err = mod.Directory.ResolveByOrgAlias(ctx, "no-vinculada")
	require.ErrorIs(t, err, contracts.ErrTenantNotFound)

	_, err = mod.Provision.Handle(ctx, app.ProvisionTenantCommand{Reason: "prueba", Operator: "ops.test", Name: "Otra", Plan: "basico", OrgAlias: alias})
	require.ErrorIs(t, err, domain.ErrOrgAliasTaken)
}

// Un tenant no ve la fila de otro aunque conozca su ID.
func TestTenantIsolation(t *testing.T) {
	t.Parallel()
	mod := newModule()
	ctx := context.Background()
	idA, err := mod.Provision.Handle(ctx, app.ProvisionTenantCommand{Reason: "prueba", Operator: "ops.test", Name: "A", Plan: "basico", OrgAlias: uniqueAlias()})
	require.NoError(t, err)
	idB, err := mod.Provision.Handle(ctx, app.ProvisionTenantCommand{Reason: "prueba", Operator: "ops.test", Name: "B", Plan: "basico", OrgAlias: uniqueAlias()})
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
	id, err := mod.Provision.Handle(ctx, app.ProvisionTenantCommand{Reason: "prueba", Operator: "ops.test", Name: "A", Plan: "basico", OrgAlias: uniqueAlias()})
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

func TestAPIRoleCannotChangeTenants(t *testing.T) {
	t.Parallel()
	id, err := newModule().Provision.Handle(context.Background(), app.ProvisionTenantCommand{Reason: "prueba", Operator: "ops.test", Name: "Agencia", Plan: "basico", OrgAlias: uniqueAlias()})
	require.NoError(t, err)
	ctx := reqctx.WithTenantID(context.Background(), id)
	err = appDB.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE tenancy.tenants SET plan = 'empresarial'`)
		return err
	})
	require.Error(t, err, "la API no se cambia el plan a sí misma")

	info, err := tenancy.New(appDB, kernel.SystemClock{}).Directory.Get(context.Background(), id)
	require.NoError(t, err, "la API sí lee su propio tenant")
	assert.Equal(t, "basico", info.Plan)
}

// Alta, plan y suspensión quedan en audit.events y messaging.outbox con el rol seguros_platform, en
// la misma transacción; si el cambio falla no queda rastro.
func TestPlatformOperationsLeaveAuditAndOutbox(t *testing.T) {
	t.Parallel()
	mod := newModule()
	alias := uniqueAlias()
	base := context.Background()
	id, err := mod.Provision.Handle(base, app.ProvisionTenantCommand{Reason: "prueba", Operator: "ops.test", Name: "A", Plan: "basico", OrgAlias: alias})
	require.NoError(t, err)
	require.NoError(t, mod.ChangePlan.Handle(base, app.ChangeTenantPlanCommand{Reason: "upgrade", Operator: "ana.ops", OrgAlias: alias, Plan: "empresarial"}))
	require.NoError(t, mod.ChangeStatus.Handle(base, app.ChangeTenantStatusCommand{Reason: "impago", Operator: "ana.ops", OrgAlias: alias, Suspend: true}))
	err = mod.ChangeStatus.Handle(base, app.ChangeTenantStatusCommand{Reason: "repetida", Operator: "ana.ops", OrgAlias: alias, Suspend: true})
	require.ErrorIs(t, err, domain.ErrInvalidTransition)

	ctx := reqctx.WithTenantID(base, id)
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT action, details->>'reason', details->>'operator', actor_type
			FROM audit.events WHERE tenant_id = $1 AND resource_type = 'tenancy.tenant' ORDER BY id`, uuid.UUID(id))
		if err != nil {
			return err
		}
		defer rows.Close()
		var got [][4]string
		for rows.Next() {
			var r [4]string
			if err := rows.Scan(&r[0], &r[1], &r[2], &r[3]); err != nil {
				return err
			}
			got = append(got, r)
		}
		assert.Equal(t, [][4]string{
			{"tenancy.tenant.provisioned", "prueba", "ops.test", "system"},
			{"tenancy.tenant.plan_changed", "upgrade", "ana.ops", "system"},
			{"tenancy.tenant.suspended", "impago", "ana.ops", "system"},
		}, got)
		return rows.Err()
	}))
	var events int
	require.NoError(t, pgSuper.QueryRow(base, `SELECT count(*) FROM messaging.outbox WHERE tenant_id = $1`, uuid.UUID(id)).Scan(&events))
	assert.Equal(t, 3, events)
}
