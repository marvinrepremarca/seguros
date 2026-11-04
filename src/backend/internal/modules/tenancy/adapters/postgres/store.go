// Package postgres implementa los puertos de persistencia de tenancy con pgx + sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	tenancysqlc "github.com/marvinrepremarca/seguros/internal/modules/tenancy/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/app"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

const uniqueViolation = "23505"

// Store implementa app.Store.
type Store struct {
	db       *db.DB
	recorder *audit.Recorder
	outbox   *outbox.Writer
}

var _ app.Store = (*Store)(nil)

// NewStore construye el Store.
func NewStore(database *db.DB, recorder *audit.Recorder, writer *outbox.Writer) *Store {
	return &Store{db: database, recorder: recorder, outbox: writer}
}

// WithinTenant implementa app.Store.
func (s *Store) WithinTenant(ctx context.Context, fn func(ctx context.Context, uow app.UnitOfWork) error) error {
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return fn(ctx, &unitOfWork{tx: tx, repo: &repository{q: tenancysqlc.New(tx)}, recorder: s.recorder, outbox: s.outbox})
	})
}

type unitOfWork struct {
	tx       pgx.Tx
	repo     *repository
	recorder *audit.Recorder
	outbox   *outbox.Writer
}

func (u *unitOfWork) Tenants() app.TenantRepository { return u.repo }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.recorder.Record(ctx, u.tx, e)
}

func (u *unitOfWork) Publish(ctx context.Context, topic string, payload any) error {
	_, err := u.outbox.Add(ctx, u.tx, topic, payload)
	return err
}

// ResolveOrgAlias implementa app.Store mediante la función SECURITY DEFINER (antes de conocer el
// tenant no hay RLS que lo permita).
func (s *Store) ResolveOrgAlias(ctx context.Context, orgAlias string) (kernel.TenantID, error) {
	var id kernel.TenantID
	err := s.db.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		row, err := tenancysqlc.New(tx).ResolveTenantByOrgAlias(ctx, orgAlias)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("tenancy: resolviendo organización: %w", err)
		}
		id = kernel.TenantID(row.ID)
		return nil
	})
	return id, err
}

type repository struct {
	q *tenancysqlc.Queries
}

func (r *repository) Insert(ctx context.Context, t *domain.Tenant) error {
	s := t.Snapshot()
	err := r.q.InsertTenant(ctx, tenancysqlc.InsertTenantParams{
		ID: uuid.UUID(s.ID), Name: s.Name, Plan: string(s.Plan), Status: string(s.Status),
		IdpOrgAlias: s.OrgAlias, StatusSince: s.StatusSince, CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt, Version: s.Version,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "tenants_idp_org_alias_key" {
		return domain.ErrOrgAliasTaken
	}
	if err != nil {
		return fmt.Errorf("tenancy: insertando tenant: %w", err)
	}
	return nil
}

func (r *repository) Get(ctx context.Context, id kernel.TenantID) (*domain.Tenant, error) {
	row, err := r.q.GetTenant(ctx, uuid.UUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("tenancy: leyendo tenant: %w", err)
	}
	return domain.RehydrateTenant(domain.TenantSnapshot{
		ID: kernel.TenantID(row.ID), Name: row.Name, Plan: domain.Plan(row.Plan), Status: domain.Status(row.Status),
		OrgAlias: row.IdpOrgAlias, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		StatusSince: row.StatusSince, Version: row.Version,
	}), nil
}

func (r *repository) Update(ctx context.Context, t *domain.Tenant) error {
	s := t.Snapshot()
	n, err := r.q.UpdateTenant(ctx, tenancysqlc.UpdateTenantParams{
		ID: uuid.UUID(s.ID), Name: s.Name, Plan: string(s.Plan), Status: string(s.Status),
		StatusSince: s.StatusSince, UpdatedAt: s.UpdatedAt, Version: s.Version,
	})
	if err != nil {
		return fmt.Errorf("tenancy: actualizando tenant: %w", err)
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	return nil
}
