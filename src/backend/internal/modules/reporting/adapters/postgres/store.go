// Package postgres lee las vistas de reporting con pgx + sqlc.
package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	reportingsqlc "github.com/marvinrepremarca/seguros/internal/modules/reporting/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/reporting/app"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Store implementa app.Store.
type Store struct{ db *db.DB }

// NewStore construye el Store.
func NewStore(database *db.DB) *Store { return &Store{db: database} }

// WithinTenant implementa app.Store.
func (s *Store) WithinTenant(ctx context.Context, fn func(ctx context.Context, r app.Repository) error) error {
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		tenant, ok := reqctx.TenantID(ctx)
		if !ok {
			return db.ErrNoTenant
		}
		return fn(ctx, &repository{q: reportingsqlc.New(tx), tenant: uuid.UUID(tenant)})
	})
}

type repository struct {
	q      *reportingsqlc.Queries
	tenant uuid.UUID
}

func wrap(err error, op string) error {
	if err != nil {
		return fmt.Errorf("reporting: %s: %w", op, err)
	}
	return nil
}

func (r *repository) Premiums(ctx context.Context, from, to kernel.Date) ([]app.Amount, error) {
	rows, err := r.q.IssuedPremiums(ctx, reportingsqlc.IssuedPremiumsParams{TenantID: r.tenant, FromDate: from, ToDate: to})
	if err != nil {
		return nil, wrap(err, "primas")
	}
	out := make([]app.Amount, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.Amount{Currency: x.Currency, Value: x.Amount})
	}
	return out, nil
}

func (r *repository) IssuanceDays(ctx context.Context, from, to kernel.Date) (decimal.Decimal, error) {
	d, err := r.q.IssuanceDays(ctx, reportingsqlc.IssuanceDaysParams{TenantID: r.tenant, FromDate: from, ToDate: to})
	return d, wrap(err, "tiempo de expedición")
}

func (r *repository) PoliciesPerClient(ctx context.Context) (policies, clients int32, err error) {
	x, err := r.q.PoliciesPerClient(ctx, r.tenant)
	return x.Policies, x.Clients, wrap(err, "pólizas por cliente")
}

func (r *repository) Conversion(ctx context.Context, from, to kernel.Date) (app.Counts, error) {
	x, err := r.q.Conversion(ctx, reportingsqlc.ConversionParams{TenantID: r.tenant, FromDate: from, ToDate: to})
	return app.Counts{Total: x.Requests, Part: x.Accepted}, wrap(err, "conversión")
}

func (r *repository) Retention(ctx context.Context, from, to kernel.Date) (app.Counts, error) {
	x, err := r.q.Retention(ctx, reportingsqlc.RetentionParams{TenantID: r.tenant, FromDate: from, ToDate: to})
	return app.Counts{Total: x.Due, Part: x.Renewed}, wrap(err, "retención")
}

func (r *repository) Overdue(ctx context.Context, today kernel.Date) ([]app.Bucket, error) {
	rows, err := r.q.OverdueByAge(ctx, reportingsqlc.OverdueByAgeParams{TenantID: r.tenant, Today: today})
	if err != nil {
		return nil, wrap(err, "cartera vencida")
	}
	out := make([]app.Bucket, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.Bucket{Name: x.Bucket, Currency: x.Currency, Balance: x.Balance})
	}
	return out, nil
}

func (r *repository) Accrued(ctx context.Context, from, to kernel.Date) ([]app.Amount, error) {
	rows, err := r.q.AccruedCommissions(ctx, reportingsqlc.AccruedCommissionsParams{TenantID: r.tenant, FromDate: from, ToDate: to})
	if err != nil {
		return nil, wrap(err, "comisiones causadas")
	}
	out := make([]app.Amount, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.Amount{Currency: x.Currency, Value: x.Amount})
	}
	return out, nil
}

func (r *repository) Received(ctx context.Context, from, to kernel.Date) ([]app.Amount, error) {
	rows, err := r.q.ReceivedCommissions(ctx, reportingsqlc.ReceivedCommissionsParams{TenantID: r.tenant, FromDate: from, ToDate: to})
	if err != nil {
		return nil, wrap(err, "comisiones recibidas")
	}
	out := make([]app.Amount, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.Amount{Currency: x.Currency, Value: x.Amount})
	}
	return out, nil
}

func (r *repository) ClaimDays(ctx context.Context, from, to kernel.Date) (decimal.Decimal, error) {
	d, err := r.q.ClaimDays(ctx, reportingsqlc.ClaimDaysParams{TenantID: r.tenant, FromDate: from, ToDate: to})
	return d, wrap(err, "tiempo de siniestros")
}

func (r *repository) PaidClaims(ctx context.Context, from, to kernel.Date) ([]app.Amount, error) {
	rows, err := r.q.PaidClaims(ctx, reportingsqlc.PaidClaimsParams{TenantID: r.tenant, FromDate: from, ToDate: to})
	if err != nil {
		return nil, wrap(err, "siniestros pagados")
	}
	out := make([]app.Amount, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.Amount{Currency: x.Currency, Value: x.Amount})
	}
	return out, nil
}
