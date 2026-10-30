// Package db da acceso a PostgreSQL. La ÚNICA forma de ejecutar SQL de negocio es
// DB.WithTenantTx, que abre una transacción y fija app.tenant_id para que apliquen las políticas
// RLS (CLAUDE.md regla 2). El pool no se expone a los módulos.
package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// ErrNoTenant indica que se intentó acceder a datos sin tenant autenticado en el contexto.
// Es un error de programación o de autenticación: nunca se usa un tenant por defecto.
var ErrNoTenant = errors.New("db: no hay tenant en el contexto")

// ErrPrivilegedRole indica que el rol de conexión puede saltarse RLS (superusuario o BYPASSRLS).
var ErrPrivilegedRole = errors.New("db: el rol de la aplicación no puede ser superusuario ni tener BYPASSRLS")

// DB envuelve el pool de conexiones del rol de la aplicación (seguros_app).
type DB struct {
	pool *pgxpool.Pool
}

// Options configura Open.
type Options struct {
	ApplicationName string
}

// Open crea el pool y verifica que el rol conectado esté sujeto a RLS (fail-closed).
func Open(ctx context.Context, cfg config.Database, opts Options) (*DB, error) {
	pc, err := pgxpool.ParseConfig(cfg.URL.Reveal())
	if err != nil {
		return nil, errors.New("db: URL de conexión inválida") // sin detalles: contiene la contraseña
	}
	pc.MaxConns = cfg.MaxConns
	pc.MinConns = cfg.MinConns
	pc.MaxConnLifetime = cfg.MaxConnLifetime
	pc.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10)
	pc.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = strconv.FormatInt(2*cfg.StatementTimeout.Milliseconds(), 10)
	if opts.ApplicationName != "" {
		pc.ConnConfig.RuntimeParams["application_name"] = opts.ApplicationName
	}

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("db: creando pool: %w", err)
	}
	d := &DB{pool: pool}
	if err := d.checkRole(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) checkRole(ctx context.Context) error {
	var super, bypass bool
	err := d.pool.QueryRow(ctx,
		`SELECT rolsuper, rolbypassrls FROM pg_catalog.pg_roles WHERE rolname = current_user`,
	).Scan(&super, &bypass)
	if err != nil {
		return fmt.Errorf("db: verificando rol: %w", err)
	}
	if super || bypass {
		return ErrPrivilegedRole
	}
	return nil
}

// Close cierra el pool.
func (d *DB) Close() { d.pool.Close() }

// Ping verifica la conectividad (para /health/ready).
func (d *DB) Ping(ctx context.Context) error {
	if err := d.pool.Ping(ctx); err != nil {
		return fmt.Errorf("db: ping: %w", err)
	}
	return nil
}

// WithTenantTx ejecuta fn en una transacción con app.tenant_id fijado (transaction-local) al
// tenant del contexto. Hace commit si fn devuelve nil y rollback si devuelve error o entra en
// pánico. Sin tenant en el contexto devuelve ErrNoTenant sin tocar la BD.
func (d *DB) WithTenantTx(ctx context.Context, fn func(tx pgx.Tx) error) (err error) {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return ErrNoTenant
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: iniciando transacción: %w", err)
	}
	defer func() {
		// Tras un commit exitoso Rollback devuelve ErrTxClosed; en cualquier otro caso deshace.
		if rbErr := tx.Rollback(context.WithoutCancel(ctx)); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("db: rollback: %w", rbErr))
		}
	}()

	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
		return fmt.Errorf("db: fijando tenant: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit: %w", err)
	}
	return nil
}
