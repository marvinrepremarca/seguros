//go:build integration

// Package rlsmeta verifica el esquema migrado completo: toda tabla con tenant_id está aislada por RLS.
package rlsmeta_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/test/pgtest"
)

func TestEveryTenantTableHasForcedRLSAndAPolicy(t *testing.T) {
	ctx := context.Background()
	env, err := pgtest.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Terminate()) })
	require.NoError(t, env.MigrateAll(ctx))

	conn, err := pgx.Connect(ctx, env.SuperuserURL)
	require.NoError(t, err)
	defer conn.Close(ctx) //nolint:errcheck // limpieza de la prueba

	rows, err := conn.Query(ctx, `
		SELECT n.nspname || '.' || c.relname, c.relrowsecurity, c.relforcerowsecurity,
		       EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'tenant_isolation')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped
		WHERE c.relkind IN ('r', 'p') AND NOT c.relispartition
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')`)
	require.NoError(t, err)
	defer rows.Close()
	checked := 0
	for rows.Next() {
		var table string
		var rls, force, policy bool
		require.NoError(t, rows.Scan(&table, &rls, &force, &policy))
		assert.True(t, rls && force, "%s sin ENABLE + FORCE ROW LEVEL SECURITY", table)
		assert.True(t, policy, "%s sin política tenant_isolation", table)
		checked++
	}
	require.NoError(t, rows.Err())
	assert.GreaterOrEqual(t, checked, 11, "debe recorrer las tablas de negocio")
}
