package migrate_test

import (
	"log/slog"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/db/migrate"
)

// Las vistas de reporting leen los esquemas de los demás: su módulo migra siempre al final (ADR 0013).
func TestReportingMigratesLast(t *testing.T) {
	t.Parallel()
	up := []byte("-- +goose Up\nSELECT 1;\n")
	fsys := fstest.MapFS{"reporting/00001_x.sql": {Data: up}, "zeta/00001_x.sql": {Data: up}, "alpha/00001_x.sql": {Data: up}}
	mods, err := migrate.New(nil, fsys, slog.New(slog.DiscardHandler)).Modules()
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "zeta", "reporting"}, mods)
}
