//go:build integration

package db_test

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/db/migrate"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/pgtest"
)

var pg *pgtest.Env //nolint:gochecknoglobals // contenedor compartido por el paquete (TestMain)

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	ctx := context.Background()
	env, err := pgtest.Start(ctx)
	if err != nil {
		slog.Error("iniciando PostgreSQL", "error", err)
		return 1
	}
	defer func() {
		if err := env.Terminate(); err != nil {
			slog.Error("deteniendo PostgreSQL", "error", err)
		}
	}()
	pg = env

	sqlDB, err := migrate.Open(env.MigratorURL)
	if err != nil {
		slog.Error("abriendo conexión de migración", "error", err)
		return 1
	}
	defer sqlDB.Close() //nolint:errcheck // cierre al final de la suite
	if err := migrate.New(sqlDB, os.DirFS("testdata/migrations"), slog.New(slog.DiscardHandler)).Up(ctx); err != nil {
		slog.Error("aplicando migraciones", "error", err)
		return 1
	}
	return m.Run()
}

func dbConfig(url string) config.Database {
	return config.Database{
		URL: config.NewSecret(url), MaxConns: 4, MinConns: 0,
		MaxConnLifetime: time.Hour, StatementTimeout: 5 * time.Second,
	}
}

func openApp(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(context.Background(), dbConfig(pg.AppURL), db.Options{ApplicationName: "db-test"})
	require.NoError(t, err)
	t.Cleanup(d.Close)
	return d
}

func newTenant(t *testing.T) context.Context {
	t.Helper()
	return reqctx.WithTenantID(context.Background(), kernel.TenantID(uuid.Must(uuid.NewV7())))
}

func tenantOf(ctx context.Context, t *testing.T) uuid.UUID {
	t.Helper()
	id, ok := reqctx.TenantID(ctx)
	require.True(t, ok)
	return uuid.UUID(id)
}

const insertSQL = `INSERT INTO dbtest.items (id, tenant_id, name, amount, currency, effective_on)
	VALUES ($1, $2, $3, 100, 'COP', '2026-10-30')`

func insertItem(ctx context.Context, t *testing.T, d *db.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	require.NoError(t, d.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, insertSQL, id, tenantOf(ctx, t), name)
		return err
	}))
	return id
}

func countItems(ctx context.Context, t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	require.NoError(t, d.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM dbtest.items`).Scan(&n)
	}))
	return n
}

func TestOpenRejectsPrivilegedRole(t *testing.T) {
	t.Parallel()
	_, err := db.Open(context.Background(), dbConfig(pg.SuperuserURL), db.Options{})
	require.ErrorIs(t, err, db.ErrPrivilegedRole)
}

func TestOpenInvalidURLDoesNotLeakIt(t *testing.T) {
	t.Parallel()
	bad := url.URL{Scheme: "postgres", User: url.UserPassword("u", "zz-sentinel"), Host: "[bad"}
	_, err := db.Open(context.Background(), dbConfig(bad.String()), db.Options{})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "zz-sentinel")
}

func TestPing(t *testing.T) {
	t.Parallel()
	require.NoError(t, openApp(t).Ping(context.Background()))
}

func TestWithTenantTxRequiresTenant(t *testing.T) {
	t.Parallel()
	d := openApp(t)
	called := false
	err := d.WithTenantTx(context.Background(), func(pgx.Tx) error {
		called = true
		return nil
	})
	require.ErrorIs(t, err, db.ErrNoTenant)
	assert.False(t, called)

	err = d.WithTenantTx(reqctx.WithTenantID(context.Background(), kernel.TenantID{}), func(pgx.Tx) error { return nil })
	require.ErrorIs(t, err, db.ErrNoTenant, "el UUID nulo no es un tenant")
}

func TestTenantIsolation(t *testing.T) {
	t.Parallel()
	d := openApp(t)
	tenantA, tenantB := newTenant(t), newTenant(t)
	idA := insertItem(tenantA, t, d, "de A")

	assert.Equal(t, 1, countItems(tenantA, t, d))
	assert.Equal(t, 0, countItems(tenantB, t, d), "B no ve datos de A")

	// B no puede modificar ni borrar filas de A aunque conozca el ID.
	var updated, deleted int64
	require.NoError(t, d.WithTenantTx(tenantB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(tenantB, `UPDATE dbtest.items SET name = 'hackeado' WHERE id = $1`, idA)
		if err != nil {
			return err
		}
		updated = tag.RowsAffected()
		tag, err = tx.Exec(tenantB, `DELETE FROM dbtest.items WHERE id = $1`, idA)
		deleted = tag.RowsAffected()
		return err
	}))
	assert.Zero(t, updated)
	assert.Zero(t, deleted)

	var name string
	require.NoError(t, d.WithTenantTx(tenantA, func(tx pgx.Tx) error {
		return tx.QueryRow(tenantA, `SELECT name FROM dbtest.items WHERE id = $1`, idA).Scan(&name)
	}))
	assert.Equal(t, "de A", name)
}

func TestWithCheckRejectsWritingForAnotherTenant(t *testing.T) {
	t.Parallel()
	d := openApp(t)
	tenantA, tenantB := newTenant(t), newTenant(t)
	err := d.WithTenantTx(tenantA, func(tx pgx.Tx) error {
		_, err := tx.Exec(tenantA, insertSQL, uuid.Must(uuid.NewV7()), tenantOf(tenantB, t), "x")
		return err
	})
	require.ErrorContains(t, err, "row-level security")
	assert.Equal(t, 0, countItems(tenantB, t, d))
}

// Sin WithTenantTx no hay acceso: una conexión directa del rol de la app no puede leer datos
// porque app.tenant_id no está fijado (ni antes ni después de una transacción con tenant).
func TestNoAccessWithoutWithTenantTx(t *testing.T) {
	t.Parallel()
	d := openApp(t)
	insertItem(newTenant(t), t, d, "x")

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, pg.AppURL)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(ctx)) })

	var n int
	err = conn.QueryRow(ctx, `SELECT count(*) FROM dbtest.items`).Scan(&n)
	require.ErrorContains(t, err, "app.tenant_id", "sin set_config la política falla")

	// Tras una transacción con tenant (set_config local), la variable vuelve a vacío.
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, uuid.Must(uuid.NewV7()).String())
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	err = conn.QueryRow(ctx, `SELECT count(*) FROM dbtest.items`).Scan(&n)
	require.Error(t, err, "app.tenant_id no persiste fuera de la transacción")
}

func TestAppRoleCannotDDLOrBypassRLS(t *testing.T) {
	t.Parallel()
	d := openApp(t)
	ctx := newTenant(t)
	for name, stmt := range map[string]string{
		"crear tabla en esquema de módulo": `CREATE TABLE dbtest.evil (id int)`,
		"crear tabla en public":            `CREATE TABLE public.evil (id int)`,
		"crear esquema":                    `CREATE SCHEMA evil`,
		"desactivar RLS":                   `ALTER TABLE dbtest.items DISABLE ROW LEVEL SECURITY`,
		"quitar política":                  `DROP POLICY tenant_isolation ON dbtest.items`,
		"leer versiones de migración":      `SELECT * FROM public.goose_dbtest`,
	} {
		err := d.WithTenantTx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, stmt)
			return err
		})
		require.Error(t, err, name)
	}

	err := d.WithTenantTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL row_security = off`); err != nil {
			return err
		}
		var n int
		return tx.QueryRow(ctx, `SELECT count(*) FROM dbtest.items`).Scan(&n)
	})
	require.ErrorContains(t, err, "row-level security", "row_security=off no salta RLS")
}

func TestWithTenantTxRollsBackOnError(t *testing.T) {
	t.Parallel()
	d := openApp(t)
	ctx := newTenant(t)
	errBoom := errors.New("boom")
	err := d.WithTenantTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, insertSQL, uuid.Must(uuid.NewV7()), tenantOf(ctx, t), "x"); err != nil {
			return err
		}
		return errBoom
	})
	require.ErrorIs(t, err, errBoom)
	assert.Equal(t, 0, countItems(ctx, t, d))
}

func TestWithTenantTxRollsBackOnPanic(t *testing.T) {
	t.Parallel()
	d := openApp(t)
	ctx := newTenant(t)
	var execErr error
	assert.Panics(t, func() {
		err := d.WithTenantTx(ctx, func(tx pgx.Tx) error {
			_, execErr = tx.Exec(ctx, insertSQL, uuid.Must(uuid.NewV7()), tenantOf(ctx, t), "x")
			panic("fallo inesperado")
		})
		t.Errorf("no debió retornar: %v", err)
	})
	require.NoError(t, execErr)
	assert.Equal(t, 0, countItems(ctx, t, d))
}

// Los tipos del kernel y decimal funcionan con pgx sobre date y numeric (overrides de sqlc).
func TestKernelTypesRoundTrip(t *testing.T) {
	t.Parallel()
	d := openApp(t)
	ctx := newTenant(t)
	effective, err := kernel.ParseDate("2028-02-29")
	require.NoError(t, err)
	amount := decimal.RequireFromString("123456789012345.6789")
	id := uuid.Must(uuid.NewV7())

	require.NoError(t, d.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO dbtest.items (id, tenant_id, name, amount, currency, effective_on)
			 VALUES ($1, $2, 'x', $3, 'COP', $4)`,
			id, tenantOf(ctx, t), amount, effective)
		return err
	}))

	var gotDate kernel.Date
	var gotAmount decimal.Decimal
	require.NoError(t, d.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT effective_on, amount FROM dbtest.items WHERE id = $1`, id).Scan(&gotDate, &gotAmount)
	}))
	assert.Equal(t, effective, gotDate)
	assert.True(t, amount.Equal(gotAmount), "got %s", gotAmount)

	money, err := kernel.NewMoney(gotAmount, kernel.COP)
	require.NoError(t, err)
	assert.Equal(t, "COP 123456789012345.6789", money.String())
}
