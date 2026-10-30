//go:build integration

package migrate_test

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/db/migrate"
	"github.com/marvinrepremarca/seguros/migrations"
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
	return m.Run()
}

func open(t *testing.T, url string) *sql.DB {
	t.Helper()
	db, err := migrate.Open(url)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func testdata() fstest.MapFS {
	return fstest.MapFS{
		"alpha/00001_init.sql": {Data: []byte(`-- +goose Up
CREATE SCHEMA alpha;
CREATE TABLE alpha.t (id int);
-- +goose Down
DROP TABLE alpha.t;
DROP SCHEMA alpha;
`)},
		"alpha/00002_col.sql": {Data: []byte(`-- +goose Up
ALTER TABLE alpha.t ADD COLUMN c text;
-- +goose Down
ALTER TABLE alpha.t DROP COLUMN c;
`)},
		"beta/00001_init.sql": {Data: []byte(`-- +goose Up
CREATE SCHEMA beta;
-- +goose Down
DROP SCHEMA beta;
`)},
		"empty/README.md": {Data: []byte("sin migraciones")},
		"embed.go":        {Data: []byte("package migrations")},
	}
}

func status(t *testing.T, r *migrate.Runner) map[string]migrate.ModuleStatus {
	t.Helper()
	sts, err := r.Status(context.Background())
	require.NoError(t, err)
	out := map[string]migrate.ModuleStatus{}
	for _, s := range sts {
		out[s.Module] = s
	}
	return out
}

func TestRunnerLifecycle(t *testing.T) {
	ctx := context.Background()
	r := migrate.New(open(t, pg.MigratorURL), testdata(), slog.New(slog.DiscardHandler))

	mods, err := r.Modules()
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "beta"}, mods, "ignora carpetas sin .sql y archivos sueltos")

	st := status(t, r)
	assert.Equal(t, migrate.ModuleStatus{Module: "alpha", Current: 0, Pending: 2}, st["alpha"])

	require.NoError(t, r.Up(ctx))
	require.NoError(t, r.Up(ctx), "idempotente")
	st = status(t, r)
	assert.Equal(t, migrate.ModuleStatus{Module: "alpha", Current: 2, Pending: 0}, st["alpha"])
	assert.Equal(t, migrate.ModuleStatus{Module: "beta", Current: 1, Pending: 0}, st["beta"])

	require.NoError(t, r.DownOne(ctx, "alpha"))
	st = status(t, r)
	assert.Equal(t, migrate.ModuleStatus{Module: "alpha", Current: 1, Pending: 1}, st["alpha"])
	assert.Equal(t, int64(1), st["beta"].Current, "down de un módulo no afecta a otros")

	require.ErrorIs(t, r.DownOne(ctx, "gamma"), migrate.ErrUnknownModule)
	require.NoError(t, r.Up(ctx))
}

func TestRunnerRejectsInvalidModuleNames(t *testing.T) {
	fsys := fstest.MapFS{"Bad-Name/00001_x.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")}}
	r := migrate.New(open(t, pg.MigratorURL), fsys, slog.New(slog.DiscardHandler))
	_, err := r.Modules()
	require.ErrorContains(t, err, "nombre de módulo inválido")
	require.Error(t, r.Up(context.Background()))
}

func TestAppRoleCannotMigrate(t *testing.T) {
	fsys := fstest.MapFS{"gamma/00001_init.sql": {Data: []byte("-- +goose Up\nCREATE SCHEMA gamma;\n-- +goose Down\nSELECT 1;\n")}}
	r := migrate.New(open(t, pg.AppURL), fsys, slog.New(slog.DiscardHandler))
	require.Error(t, r.Up(context.Background()), "seguros_app no tiene DDL")
}

// Las migraciones reales del repositorio aplican desde cero.
func TestRepositoryMigrationsApply(t *testing.T) {
	r := migrate.New(open(t, pg.MigratorURL), migrations.FS, slog.New(slog.DiscardHandler))
	require.NoError(t, r.Up(context.Background()))
}
