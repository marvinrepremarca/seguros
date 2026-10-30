// Package migrate aplica las migraciones goose de cada módulo. Cada módulo es una carpeta de la
// raíz del FS (migrations/<modulo>/NNNNN_desc.sql) con su propia tabla de versiones
// (public.goose_<modulo>), de modo que las versiones de un módulo no dependen de las de otro.
// Se ejecuta solo con el rol seguros_migrator desde cmd/migrate (nunca al arrancar la API).
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"regexp"
	"slices"

	_ "github.com/jackc/pgx/v5/stdlib" // driver database/sql "pgx" que exige goose
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

var modulePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}$`) //nolint:gochecknoglobals // regexp inmutable

// ErrUnknownModule indica un módulo sin carpeta de migraciones.
var ErrUnknownModule = errors.New("migrate: módulo sin migraciones")

// Runner aplica migraciones por módulo.
type Runner struct {
	db     *sql.DB
	fsys   fs.FS
	logger *slog.Logger
}

// ModuleStatus es el estado de un módulo.
type ModuleStatus struct {
	Module  string
	Current int64
	Pending int
}

// Open abre la conexión database/sql que necesita goose.
func Open(url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, errors.New("migrate: URL de conexión inválida") // sin detalles: contiene la contraseña
	}
	return db, nil
}

// New crea un Runner sobre fsys (raíz con una carpeta por módulo).
func New(db *sql.DB, fsys fs.FS, logger *slog.Logger) *Runner {
	return &Runner{db: db, fsys: fsys, logger: logger}
}

// Modules lista los módulos con al menos un archivo .sql, en orden alfabético.
func (r *Runner) Modules() ([]string, error) {
	entries, err := fs.ReadDir(r.fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("migrate: leyendo migraciones: %w", err)
	}
	var mods []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !modulePattern.MatchString(e.Name()) {
			return nil, fmt.Errorf("migrate: nombre de módulo inválido %q", e.Name())
		}
		sqlFiles, err := fs.Glob(r.fsys, path.Join(e.Name(), "*.sql"))
		if err != nil {
			return nil, fmt.Errorf("migrate: %s: %w", e.Name(), err)
		}
		if len(sqlFiles) > 0 {
			mods = append(mods, e.Name())
		}
	}
	slices.Sort(mods)
	return mods, nil
}

func (r *Runner) provider(module string) (*goose.Provider, error) {
	sub, err := fs.Sub(r.fsys, module)
	if err != nil {
		return nil, fmt.Errorf("migrate: %s: %w", module, err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("migrate: creando lock: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, r.db, sub,
		goose.WithTableName("public.goose_"+module),
		goose.WithSessionLocker(locker),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return nil, fmt.Errorf("migrate: %s: %w", module, err)
	}
	return p, nil
}

// Up aplica todas las migraciones pendientes de todos los módulos.
func (r *Runner) Up(ctx context.Context) error {
	mods, err := r.Modules()
	if err != nil {
		return err
	}
	for _, m := range mods {
		p, err := r.provider(m)
		if err != nil {
			return err
		}
		results, err := p.Up(ctx)
		if err != nil {
			return fmt.Errorf("migrate: %s up: %w", m, err)
		}
		for _, res := range results {
			r.logger.InfoContext(ctx, "migración aplicada", "module", m, "file", res.Source.Path, "duration", res.Duration)
		}
	}
	return nil
}

// DownOne revierte la última migración aplicada de un módulo.
func (r *Runner) DownOne(ctx context.Context, module string) error {
	mods, err := r.Modules()
	if err != nil {
		return err
	}
	if !slices.Contains(mods, module) {
		return fmt.Errorf("%w: %q", ErrUnknownModule, module)
	}
	p, err := r.provider(module)
	if err != nil {
		return err
	}
	res, err := p.Down(ctx)
	if err != nil {
		return fmt.Errorf("migrate: %s down: %w", module, err)
	}
	r.logger.InfoContext(ctx, "migración revertida", "module", module, "file", res.Source.Path)
	return nil
}

// Status devuelve la versión actual y las migraciones pendientes de cada módulo.
func (r *Runner) Status(ctx context.Context) ([]ModuleStatus, error) {
	mods, err := r.Modules()
	if err != nil {
		return nil, err
	}
	out := make([]ModuleStatus, 0, len(mods))
	for _, m := range mods {
		p, err := r.provider(m)
		if err != nil {
			return nil, err
		}
		statuses, err := p.Status(ctx)
		if err != nil {
			return nil, fmt.Errorf("migrate: %s status: %w", m, err)
		}
		st := ModuleStatus{Module: m}
		for _, s := range statuses {
			if s.State == goose.StatePending {
				st.Pending++
			} else if s.Source.Version > st.Current {
				st.Current = s.Source.Version
			}
		}
		out = append(out, st)
	}
	return out, nil
}
