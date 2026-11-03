// Package postgres implementa la persistencia de featuremgmt (pgx + sqlc): los puertos de app y el
// flags.Store que alimenta al evaluador de la plataforma.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	featuremgmtsqlc "github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/app"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Store implementa app.Store y flags.Store.
type Store struct {
	db       *db.DB
	recorder *audit.Recorder
	outbox   *outbox.Writer
}

var (
	_ app.Store   = (*Store)(nil)
	_ flags.Store = (*Store)(nil)
)

// NewStore construye el Store.
func NewStore(database *db.DB, recorder *audit.Recorder, writer *outbox.Writer) *Store {
	return &Store{db: database, recorder: recorder, outbox: writer}
}

// WithinTenant implementa app.Store.
func (s *Store) WithinTenant(ctx context.Context, fn func(ctx context.Context, uow app.UnitOfWork) error) error {
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return fn(ctx, &unitOfWork{tx: tx, q: featuremgmtsqlc.New(tx), recorder: s.recorder, outbox: s.outbox})
	})
}

// TenantOverrides implementa flags.Store: lee bajo RLS del propio tenant.
func (s *Store) TenantOverrides(ctx context.Context, tenant kernel.TenantID) (map[flags.Key]bool, error) {
	out := map[flags.Key]bool{}
	ctx = reqctx.WithTenantID(ctx, tenant)
	err := s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		rows, err := featuremgmtsqlc.New(tx).ListTenantOverrides(ctx, uuid.UUID(tenant))
		if err != nil {
			return fmt.Errorf("featuremgmt: leyendo overrides: %w", err)
		}
		for _, r := range rows {
			out[flags.Key(r.FlagKey)] = r.Enabled
		}
		return nil
	})
	return out, err
}

// GlobalSwitches implementa app.Store y flags.Store. La tabla no es de un tenant (sin RLS).
func (s *Store) GlobalSwitches(ctx context.Context) (map[flags.Key]bool, error) {
	out := map[flags.Key]bool{}
	err := s.db.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		rows, err := featuremgmtsqlc.New(tx).ListGlobalSwitches(ctx)
		if err != nil {
			return fmt.Errorf("featuremgmt: leyendo switches globales: %w", err)
		}
		for _, r := range rows {
			out[flags.Key(r.FlagKey)] = r.Enabled
		}
		return nil
	})
	return out, err
}

// SetGlobalSwitch implementa app.Store. Requiere el rol seguros_platform (cmd/platformctl): la
// API y el worker no tienen permiso de escritura sobre los switches globales.
func (s *Store) SetGlobalSwitch(ctx context.Context, key flags.Key, enabled *bool, reason, operator string, now time.Time) error {
	return s.db.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		q := featuremgmtsqlc.New(tx)
		previous := pgtype.Bool{}
		prev, err := q.GetGlobalSwitchForUpdate(ctx, string(key))
		switch {
		case err == nil:
			previous = pgtype.Bool{Bool: prev, Valid: true}
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("featuremgmt: leyendo switch global: %w", err)
		}
		next := pgtype.Bool{}
		if enabled == nil {
			if _, err := q.DeleteGlobalSwitch(ctx, string(key)); err != nil {
				return fmt.Errorf("featuremgmt: borrando switch global: %w", err)
			}
		} else {
			next = pgtype.Bool{Bool: *enabled, Valid: true}
			if err := q.UpsertGlobalSwitch(ctx, featuremgmtsqlc.UpsertGlobalSwitchParams{
				FlagKey: string(key), Enabled: *enabled, Reason: reason, Now: now,
				Operator: pgtype.Text{String: operator, Valid: true},
			}); err != nil {
				return fmt.Errorf("featuremgmt: guardando switch global: %w", err)
			}
		}
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("featuremgmt: id de historial: %w", err)
		}
		if err := q.InsertGlobalSwitchHistory(ctx, featuremgmtsqlc.InsertGlobalSwitchHistoryParams{
			ID: id, FlagKey: string(key), Previous: previous, New: next, Reason: reason, Operator: operator, Now: now,
		}); err != nil {
			return fmt.Errorf("featuremgmt: historial de switch global: %w", err)
		}
		return nil
	})
}

type unitOfWork struct {
	tx       pgx.Tx
	q        *featuremgmtsqlc.Queries
	recorder *audit.Recorder
	outbox   *outbox.Writer
}

func (u *unitOfWork) Publish(ctx context.Context, topic string, payload any) error {
	_, err := u.outbox.Add(ctx, u.tx, topic, payload)
	return err
}

func (u *unitOfWork) Overrides() app.OverrideRepository { return &overrides{q: u.q} }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.recorder.Record(ctx, u.tx, e)
}

type overrides struct {
	q *featuremgmtsqlc.Queries
}

func (r *overrides) List(ctx context.Context, tenant kernel.TenantID) (map[flags.Key]app.Override, error) {
	rows, err := r.q.ListTenantOverrides(ctx, uuid.UUID(tenant))
	if err != nil {
		return nil, fmt.Errorf("featuremgmt: leyendo overrides: %w", err)
	}
	out := make(map[flags.Key]app.Override, len(rows))
	for _, row := range rows {
		out[flags.Key(row.FlagKey)] = app.Override{Enabled: row.Enabled, Reason: row.Reason, UpdatedAt: row.UpdatedAt}
	}
	return out, nil
}

func (r *overrides) Upsert(ctx context.Context, tenant kernel.TenantID, key flags.Key, enabled bool, reason string, now time.Time) error {
	actor := uuid.NullUUID{}
	if u, ok := reqctx.UserID(ctx); ok {
		actor = uuid.NullUUID{UUID: uuid.UUID(u), Valid: true}
	}
	if err := r.q.UpsertTenantOverride(ctx, featuremgmtsqlc.UpsertTenantOverrideParams{
		TenantID: uuid.UUID(tenant), FlagKey: string(key), Enabled: enabled, Reason: reason, Now: now, Actor: actor,
	}); err != nil {
		return fmt.Errorf("featuremgmt: guardando override: %w", err)
	}
	return nil
}

func (r *overrides) Delete(ctx context.Context, tenant kernel.TenantID, key flags.Key) error {
	if _, err := r.q.DeleteTenantOverride(ctx, featuremgmtsqlc.DeleteTenantOverrideParams{TenantID: uuid.UUID(tenant), FlagKey: string(key)}); err != nil {
		return fmt.Errorf("featuremgmt: borrando override: %w", err)
	}
	return nil
}
