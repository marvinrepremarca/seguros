package migrate

import (
	"context"
	"fmt"

	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
)

// RiverSchema es el esquema de las tablas de River (lo crea migrations/river).
const RiverSchema = "river"

// upRiver aplica las migraciones propias de River (edición OSS) en su esquema, con su propia tabla
// de versiones (river.river_migration). Corre después de goose, que crea el esquema y los permisos
// por defecto para seguros_app.
func (r *Runner) upRiver(ctx context.Context) error {
	m, err := rivermigrate.New(riverdatabasesql.New(r.db), &rivermigrate.Config{Schema: RiverSchema, Logger: r.logger})
	if err != nil {
		return fmt.Errorf("migrate: river: %w", err)
	}
	res, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("migrate: river up: %w", err)
	}
	for _, v := range res.Versions {
		r.logger.InfoContext(ctx, "migración aplicada", "module", "river", "version", v.Version, "duration", v.Duration)
	}
	return nil
}
