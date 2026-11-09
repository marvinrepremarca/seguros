// Package postgres implementa la persistencia de notificaciones con pgx + sqlc.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	notificationssqlc "github.com/marvinrepremarca/seguros/internal/modules/notifications/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/notifications/app"
	"github.com/marvinrepremarca/seguros/internal/modules/notifications/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Store implementa app.Store.
type Store struct {
	db       *db.DB
	recorder *audit.Recorder
}

// NewStore construye el Store.
func NewStore(database *db.DB, recorder *audit.Recorder) *Store {
	return &Store{db: database, recorder: recorder}
}

func (s *Store) bind(ctx context.Context, tx pgx.Tx) (*unitOfWork, error) {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return nil, db.ErrNoTenant
	}
	return &unitOfWork{tx: tx, store: s, repo: &repository{q: notificationssqlc.New(tx), tenant: uuid.UUID(tenant)}}, nil
}

// WithinTenant implementa app.Store.
func (s *Store) WithinTenant(ctx context.Context, fn func(ctx context.Context, uow app.UnitOfWork) error) error {
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		uow, err := s.bind(ctx, tx)
		if err != nil {
			return err
		}
		return fn(ctx, uow)
	})
}

// Bind da la unidad de trabajo sobre la transacción de un consumidor.
func (s *Store) Bind(ctx context.Context, tx pgx.Tx) (app.UnitOfWork, error) { return s.bind(ctx, tx) }

// TenantsWithPendingMessages lista los tenants con avisos por enviar.
func (s *Store) TenantsWithPendingMessages(ctx context.Context) ([]kernel.TenantID, error) {
	var out []kernel.TenantID
	err := s.db.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		ids, err := notificationssqlc.New(tx).TenantsWithPendingMessages(ctx)
		if err != nil {
			return fmt.Errorf("notifications: tenants con avisos: %w", err)
		}
		for _, id := range ids {
			out = append(out, kernel.TenantID(id))
		}
		return nil
	})
	return out, err
}

type unitOfWork struct {
	tx    pgx.Tx
	store *Store
	repo  *repository
}

func (u *unitOfWork) Records() app.Repository { return u.repo }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.store.recorder.Record(ctx, u.tx, e)
}

type repository struct {
	q      *notificationssqlc.Queries
	tenant uuid.UUID
}

func wrap(err error, op string) error {
	if err != nil {
		return fmt.Errorf("notifications: %s: %w", op, err)
	}
	return nil
}

func toMessages(rows []notificationssqlc.NotificationsMessage) []app.Message {
	out := make([]app.Message, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.Message{ID: x.ID, PolicyID: x.PolicyID, Template: x.Template, Channel: x.Channel, Params: x.Params, Status: x.Status, SkipReason: x.SkipReason.String, Attempts: x.Attempts, CreatedAt: x.CreatedAt, SentAt: x.SentAt})
	}
	return out
}

func (r *repository) Insert(ctx context.Context, id uuid.UUID, template string, policy uuid.UUID, params []byte, dedupe string, at time.Time) error {
	return wrap(r.q.InsertMessage(ctx, notificationssqlc.InsertMessageParams{ID: id, TenantID: r.tenant, Template: template, PolicyID: policy, Params: params, DedupeKey: dedupe, At: at}), "guardando aviso")
}

func (r *repository) Claim(ctx context.Context, now, leaseUntil time.Time, limit int32) ([]app.Message, error) {
	rows, err := r.q.ClaimPending(ctx, notificationssqlc.ClaimPendingParams{LeaseUntil: &leaseUntil, TenantID: r.tenant, Now: &now, RowLimit: limit})
	return toMessages(rows), wrap(err, "listando pendientes")
}

func (r *repository) MarkSent(ctx context.Context, id, party uuid.UUID, at time.Time) error {
	return wrap(r.q.MarkSent(ctx, notificationssqlc.MarkSentParams{PartyID: uuid.NullUUID{UUID: party, Valid: true}, At: &at, TenantID: r.tenant, ID: id}), "marcando enviado")
}

func (r *repository) MarkSkipped(ctx context.Context, id uuid.UUID, party uuid.NullUUID, reason domain.Skip, at time.Time) error {
	return wrap(r.q.MarkSkipped(ctx, notificationssqlc.MarkSkippedParams{PartyID: party, SkipReason: pgtype.Text{String: string(reason), Valid: true}, At: at, TenantID: r.tenant, ID: id}), "marcando omitido")
}

func (r *repository) MarkAttempt(ctx context.Context, id uuid.UUID, status string, attempts int32, next *time.Time, at time.Time) error {
	return wrap(r.q.MarkAttempt(ctx, notificationssqlc.MarkAttemptParams{Status: status, Attempts: attempts, NextAttemptAt: next, At: at, TenantID: r.tenant, ID: id}), "contando intento")
}

func (r *repository) Retry(ctx context.Context, id uuid.UUID, at time.Time) error {
	actor := uuid.NullUUID{}
	if u, ok := reqctx.UserID(ctx); ok {
		actor = uuid.NullUUID{UUID: uuid.UUID(u), Valid: true}
	}
	n, err := r.q.Retry(ctx, notificationssqlc.RetryParams{At: at, Actor: actor, TenantID: r.tenant, ID: id})
	if err != nil {
		return wrap(err, "reintentando")
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *repository) List(ctx context.Context, before uuid.NullUUID, limit int32) ([]app.Message, error) {
	rows, err := r.q.ListMessages(ctx, notificationssqlc.ListMessagesParams{TenantID: r.tenant, Before: before, RowLimit: limit})
	return toMessages(rows), wrap(err, "listando avisos")
}
