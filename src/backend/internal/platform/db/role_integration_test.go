//go:build integration

package db_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/db"
)

func TestRequireRole(t *testing.T) {
	ctx := context.Background()
	app := openApp(t)
	require.NoError(t, app.RequireRole(ctx, "seguros_app", "seguros_worker", "seguros_platform"))
	require.ErrorIs(t, app.RequireRole(ctx, "seguros_worker"), db.ErrUnexpectedRole)

	worker, err := db.Open(ctx, dbConfig(pg.WorkerURL), db.Options{ApplicationName: "db-test"})
	require.NoError(t, err)
	t.Cleanup(worker.Close)
	require.NoError(t, worker.RequireRole(ctx, "seguros_worker"))
	require.ErrorIs(t, worker.RequireRole(ctx, "seguros_app", "seguros_worker", "seguros_platform"), db.ErrUnexpectedRole,
		"la API configurada con la credencial del worker no arranca")
}
