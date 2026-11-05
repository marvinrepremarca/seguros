// Package postgres implementa la persistencia de renovaciones con pgx + sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	renewalssqlc "github.com/marvinrepremarca/seguros/internal/modules/renewals/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals/app"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Store implementa app.Store.
type Store struct {
	db       *db.DB
	recorder *audit.Recorder
	outbox   *outbox.Writer
}

// NewStore construye el Store.
func NewStore(database *db.DB, recorder *audit.Recorder, writer *outbox.Writer) *Store {
	return &Store{db: database, recorder: recorder, outbox: writer}
}

// WithinTenant implementa app.Store.
func (s *Store) WithinTenant(ctx context.Context, fn func(ctx context.Context, uow app.UnitOfWork) error) error {
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		uow, err := s.Bind(ctx, tx)
		if err != nil {
			return err
		}
		return fn(ctx, uow)
	})
}

// Bind da la unidad de trabajo sobre la transacción de un consumidor, que incluye el inbox.
func (s *Store) Bind(ctx context.Context, tx pgx.Tx) (app.UnitOfWork, error) {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return nil, db.ErrNoTenant
	}
	return &unitOfWork{tx: tx, store: s, repo: &repository{q: renewalssqlc.New(tx), tenant: uuid.UUID(tenant)}}, nil
}

// TenantsWithOpenCases lista los tenants con casos por alertar (función SECURITY DEFINER, solo worker).
func (s *Store) TenantsWithOpenCases(ctx context.Context, until kernel.Date) ([]kernel.TenantID, error) {
	var out []kernel.TenantID
	err := s.db.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		ids, err := renewalssqlc.New(tx).TenantsWithOpenCases(ctx, until)
		if err != nil {
			return fmt.Errorf("renewals: tenants con casos: %w", err)
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

func (u *unitOfWork) Renewals() app.Repository { return u.repo }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.store.recorder.Record(ctx, u.tx, e)
}

func (u *unitOfWork) Publish(ctx context.Context, topic string, payload any) error {
	_, err := u.store.outbox.Add(ctx, u.tx, topic, payload)
	return err
}

type repository struct {
	q      *renewalssqlc.Queries
	tenant uuid.UUID
}

func actor(ctx context.Context) uuid.NullUUID {
	if u, ok := reqctx.UserID(ctx); ok {
		return uuid.NullUUID{UUID: uuid.UUID(u), Valid: true}
	}
	return uuid.NullUUID{}
}

func wrap(err error, op string) error {
	if err != nil {
		return fmt.Errorf("renewals: %s: %w", op, err)
	}
	return nil
}

func (r *repository) InsertCase(ctx context.Context, s domain.Snapshot, at time.Time) error {
	return wrap(r.q.InsertCase(ctx, renewalssqlc.InsertCaseParams{
		ID: s.ID, TenantID: r.tenant, PolicyID: s.PolicyID, PolicyNumber: s.PolicyNumber, InsurerID: s.InsurerID, HolderPartyID: s.HolderPartyID, EffectiveTo: s.EffectiveTo, At: at,
	}), "creando caso")
}

func (r *repository) GetCase(ctx context.Context, policyID uuid.UUID) (*domain.Case, error) {
	x, err := r.q.GetCase(ctx, renewalssqlc.GetCaseParams{TenantID: r.tenant, PolicyID: policyID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrap(err, "leyendo caso")
	}
	return domain.Rehydrate(domain.Snapshot{
		ID: x.ID, PolicyID: x.PolicyID, PolicyNumber: x.PolicyNumber, InsurerID: x.InsurerID, HolderPartyID: x.HolderPartyID, EffectiveTo: x.EffectiveTo,
		Status: domain.Status(x.Status), LostReason: x.LostReason.String, RenewedBy: x.RenewedBy, Version: x.Version,
	}), nil
}

func (r *repository) SaveCase(ctx context.Context, c *domain.Case, at time.Time) error {
	s := c.Snapshot()
	n, err := r.q.UpdateCase(ctx, renewalssqlc.UpdateCaseParams{
		TenantID: r.tenant, PolicyID: s.PolicyID, Status: string(s.Status), LostReason: pgtype.Text{String: s.LostReason, Valid: s.LostReason != ""},
		RenewedBy: s.RenewedBy, At: at, Actor: actor(ctx), Version: s.Version,
	})
	if err != nil {
		return wrap(err, "guardando caso")
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	return nil
}

func (r *repository) ListCases(ctx context.Context, status string, after uuid.NullUUID, limit int32) ([]domain.Snapshot, error) {
	rows, err := r.q.ListCases(ctx, renewalssqlc.ListCasesParams{TenantID: r.tenant, Status: pgtype.Text{String: status, Valid: status != ""}, After: after, PageSize: limit})
	if err != nil {
		return nil, wrap(err, "listando casos")
	}
	out := make([]domain.Snapshot, 0, len(rows))
	for _, x := range rows {
		out = append(out, domain.Snapshot{
			ID: x.ID, PolicyID: x.PolicyID, PolicyNumber: x.PolicyNumber, InsurerID: x.InsurerID, HolderPartyID: x.HolderPartyID, EffectiveTo: x.EffectiveTo,
			Status: domain.Status(x.Status), LostReason: x.LostReason.String, RenewedBy: x.RenewedBy, Version: x.Version,
		})
	}
	return out, nil
}

func (r *repository) OpenCasesUntil(ctx context.Context, today, until kernel.Date) ([]app.DueCase, error) {
	rows, err := r.q.OpenCasesUntil(ctx, renewalssqlc.OpenCasesUntilParams{TenantID: r.tenant, Today: today, Until: until})
	if err != nil {
		return nil, wrap(err, "listando casos por alertar")
	}
	out := make([]app.DueCase, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.DueCase{PolicyID: x.PolicyID, EffectiveTo: x.EffectiveTo})
	}
	return out, nil
}

func (r *repository) SentAlerts(ctx context.Context, policyID uuid.UUID) ([]int32, error) {
	out, err := r.q.SentAlerts(ctx, renewalssqlc.SentAlertsParams{TenantID: r.tenant, PolicyID: policyID})
	return out, wrap(err, "leyendo alertas")
}

func (r *repository) InsertAlert(ctx context.Context, policyID uuid.UUID, days int32, at time.Time) error {
	return wrap(r.q.InsertAlert(ctx, renewalssqlc.InsertAlertParams{TenantID: r.tenant, PolicyID: policyID, Days: days, SentAt: at}), "guardando alerta")
}

func (r *repository) AlertDays(ctx context.Context) ([]int32, error) {
	out, err := r.q.GetAlertDays(ctx, r.tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultAlertDays(), nil
	}
	return out, wrap(err, "leyendo días de alerta")
}

func (r *repository) SetAlertDays(ctx context.Context, days []int32, at time.Time) error {
	return wrap(r.q.SetAlertDays(ctx, renewalssqlc.SetAlertDaysParams{TenantID: r.tenant, AlertDays: days, At: at, Actor: actor(ctx)}), "guardando días de alerta")
}

func (r *repository) Retention(ctx context.Context, from, to kernel.Date) (int32, int32, error) {
	row, err := r.q.Retention(ctx, renewalssqlc.RetentionParams{TenantID: r.tenant, FromDate: from, ToDate: to})
	if err != nil {
		return 0, 0, wrap(err, "calculando retención")
	}
	return row.Due, row.Renewed, nil
}
