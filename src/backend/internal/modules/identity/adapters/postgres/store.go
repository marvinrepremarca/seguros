// Package postgres implementa los puertos de persistencia de identity con pgx + sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	identitysqlc "github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/app"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

const uniqueViolation = "23505"

// Store implementa app.Store.
type Store struct {
	db       *db.DB
	recorder *audit.Recorder
}

var _ app.Store = (*Store)(nil)

// NewStore construye el Store.
func NewStore(database *db.DB, recorder *audit.Recorder) *Store {
	return &Store{db: database, recorder: recorder}
}

// WithinTenant implementa app.Store.
func (s *Store) WithinTenant(ctx context.Context, fn func(ctx context.Context, uow app.UnitOfWork) error) error {
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return fn(ctx, &unitOfWork{tx: tx, q: identitysqlc.New(tx), recorder: s.recorder})
	})
}

type unitOfWork struct {
	tx       pgx.Tx
	q        *identitysqlc.Queries
	recorder *audit.Recorder
}

func (u *unitOfWork) Users() app.UserRepository { return &users{q: u.q} }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.recorder.Record(ctx, u.tx, e)
}

type users struct {
	q *identitysqlc.Queries
}

func (r *users) GetBySubject(ctx context.Context, tenant kernel.TenantID, subject string) (*domain.User, error) {
	row, err := r.q.GetUserBySubject(ctx, identitysqlc.GetUserBySubjectParams{TenantID: uuid.UUID(tenant), IdpSubject: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("identity: leyendo usuario: %w", err)
	}
	return domain.RehydrateUser(kernel.UserID(row.ID), kernel.TenantID(row.TenantID), row.IdpSubject,
		domain.Profile{Email: row.Email.String, DisplayName: row.DisplayName},
		row.LastLoginAt, row.CreatedAt, row.UpdatedAt, row.Version), nil
}

func (r *users) Insert(ctx context.Context, u *domain.User) error {
	err := r.q.InsertUser(ctx, identitysqlc.InsertUserParams{
		ID: uuid.UUID(u.ID()), TenantID: uuid.UUID(u.TenantID()), IdpSubject: u.Subject(),
		Email: text(u.Profile().Email), DisplayName: u.Profile().DisplayName, LastLoginAt: u.LastLoginAt(),
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrConcurrentUpdate
	}
	if err != nil {
		return fmt.Errorf("identity: insertando usuario: %w", err)
	}
	return nil
}

func (r *users) Update(ctx context.Context, u *domain.User) error {
	n, err := r.q.UpdateUserLogin(ctx, identitysqlc.UpdateUserLoginParams{
		Email: text(u.Profile().Email), DisplayName: u.Profile().DisplayName, LastLoginAt: u.LastLoginAt(),
		TenantID: uuid.UUID(u.TenantID()), ID: uuid.UUID(u.ID()), Version: u.Version(),
	})
	if err != nil {
		return fmt.Errorf("identity: actualizando usuario: %w", err)
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	return nil
}

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }
