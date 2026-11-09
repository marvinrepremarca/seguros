// Package postgres implementa la persistencia de billing con pgx + sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	billingsqlc "github.com/marvinrepremarca/seguros/internal/modules/billing/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/billing/app"
	"github.com/marvinrepremarca/seguros/internal/modules/billing/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

const uniqueViolation = "23505"

// Store implementa app.Store.
type Store struct {
	db       *db.DB
	recorder *audit.Recorder
	outbox   *outbox.Writer
	clock    kernel.Clock
}

var _ app.Store = (*Store)(nil)

// NewStore construye el Store.
func NewStore(database *db.DB, recorder *audit.Recorder, writer *outbox.Writer, clock kernel.Clock) *Store {
	return &Store{db: database, recorder: recorder, outbox: writer, clock: clock}
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
	return &unitOfWork{tx: tx, store: s, repo: &repository{q: billingsqlc.New(tx), tenant: uuid.UUID(tenant), clock: s.clock}}, nil
}

// TenantsWithInstallmentsDueOn lista los tenants con cuotas por recordar (función SECURITY DEFINER, solo worker).
func (s *Store) TenantsWithInstallmentsDueOn(ctx context.Context, dates []kernel.Date) ([]kernel.TenantID, error) {
	var out []kernel.TenantID
	err := s.db.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		ids, err := billingsqlc.New(tx).TenantsWithInstallmentsDueOn(ctx, dates)
		if err != nil {
			return fmt.Errorf("billing: tenants con cuotas por recordar: %w", err)
		}
		for _, id := range ids {
			out = append(out, kernel.TenantID(id))
		}
		return nil
	})
	return out, err
}

// TenantsWithDue lista los tenants con cuotas por revisar (función SECURITY DEFINER, solo worker).
func (s *Store) TenantsWithDue(ctx context.Context, today kernel.Date) ([]kernel.TenantID, error) {
	var out []kernel.TenantID
	err := s.db.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		ids, err := billingsqlc.New(tx).TenantsWithDueInstallments(ctx, today)
		if err != nil {
			return fmt.Errorf("billing: tenants con cuotas: %w", err)
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

func (u *unitOfWork) Billing() app.Repository { return u.repo }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.store.recorder.Record(ctx, u.tx, e)
}

func (u *unitOfWork) Publish(ctx context.Context, topic string, payload any) error {
	_, err := u.store.outbox.Add(ctx, u.tx, topic, payload)
	return err
}

type repository struct {
	q      *billingsqlc.Queries
	tenant uuid.UUID
	clock  kernel.Clock
}

func actor(ctx context.Context) uuid.NullUUID {
	if u, ok := reqctx.UserID(ctx); ok {
		return uuid.NullUUID{UUID: uuid.UUID(u), Valid: true}
	}
	return uuid.NullUUID{}
}

func (r *repository) writeInstallments(ctx context.Context, planID uuid.UUID, installments []domain.Installment) error {
	keep := make([]uuid.UUID, 0, len(installments))
	for _, i := range installments {
		keep = append(keep, i.ID)
	}
	if err := r.q.DeleteInstallments(ctx, billingsqlc.DeleteInstallmentsParams{TenantID: r.tenant, PlanID: planID, Keep: keep}); err != nil {
		return fmt.Errorf("billing: borrando cuotas: %w", err)
	}
	for _, i := range installments {
		err := r.q.UpsertInstallment(ctx, billingsqlc.UpsertInstallmentParams{
			ID: i.ID, TenantID: r.tenant, PlanID: planID, Number: i.Number, DueDate: i.DueDate, Amount: i.Amount.Amount(), Paid: i.Paid.Amount(),
			Status: string(i.Status), OverdueNotified: i.OverdueNotified,
		})
		if err != nil {
			return fmt.Errorf("billing: guardando cuota: %w", err)
		}
	}
	return nil
}

func (r *repository) InsertPlan(ctx context.Context, p *domain.PaymentPlan) error {
	s := p.Snapshot()
	err := r.q.InsertPlan(ctx, billingsqlc.InsertPlanParams{
		ID: s.ID, TenantID: r.tenant, PolicyID: s.PolicyID, PolicyNumber: s.PolicyNumber, InsurerID: s.InsurerID, HolderPartyID: s.HolderPartyID,
		Currency: string(s.Total.Currency()), Total: s.Total.Amount(), Credit: s.Credit.Amount(), At: r.clock.Now().UTC(), Actor: actor(ctx),
	})
	if err != nil {
		return fmt.Errorf("billing: creando plan: %w", err)
	}
	return r.writeInstallments(ctx, s.ID, s.Installments)
}

func (r *repository) GetPlan(ctx context.Context, policyID uuid.UUID) (*domain.PaymentPlan, error) {
	row, err := r.q.GetPlanByPolicy(ctx, billingsqlc.GetPlanByPolicyParams{TenantID: r.tenant, PolicyID: policyID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, app.ErrPlanNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("billing: leyendo plan: %w", err)
	}
	currency := kernel.Currency(row.Currency)
	money := func(d decimal.Decimal) (kernel.Money, error) { return kernel.NewMoney(d, currency) }
	total, err := money(row.Total)
	if err != nil {
		return nil, err
	}
	credit, err := money(row.Credit)
	if err != nil {
		return nil, err
	}
	var refund *domain.Refund
	if row.RefundDate != nil && row.RefundAmount.Valid {
		amount, err := money(row.RefundAmount.Decimal)
		if err != nil {
			return nil, err
		}
		refund = &domain.Refund{EffectiveDate: *row.RefundDate, Amount: amount}
	}
	rows, err := r.q.ListInstallments(ctx, billingsqlc.ListInstallmentsParams{TenantID: r.tenant, PlanID: row.ID})
	if err != nil {
		return nil, fmt.Errorf("billing: leyendo cuotas: %w", err)
	}
	installments := make([]domain.Installment, 0, len(rows))
	for _, i := range rows {
		amount, err := money(i.Amount)
		if err != nil {
			return nil, err
		}
		paid, err := money(i.Paid)
		if err != nil {
			return nil, err
		}
		installments = append(installments, domain.Installment{
			ID: i.ID, Number: i.Number, DueDate: i.DueDate, Amount: amount, Paid: paid, Status: domain.InstallmentStatus(i.Status), OverdueNotified: i.OverdueNotified,
		})
	}
	return domain.RehydratePlan(domain.PlanSnapshot{
		ID: row.ID, PolicyID: row.PolicyID, PolicyNumber: row.PolicyNumber, InsurerID: row.InsurerID, HolderPartyID: row.HolderPartyID,
		Total: total, Credit: credit, Cancelled: row.Cancelled, Refund: refund, Installments: installments, Version: row.Version,
	}), nil
}

func (r *repository) SavePlan(ctx context.Context, p *domain.PaymentPlan) error {
	s := p.Snapshot()
	params := billingsqlc.UpdatePlanParams{
		ID: s.ID, TenantID: r.tenant, Total: s.Total.Amount(), Credit: s.Credit.Amount(), Cancelled: s.Cancelled,
		At: r.clock.Now().UTC(), Actor: actor(ctx), Version: s.Version,
	}
	if s.Refund != nil {
		params.RefundDate = &s.Refund.EffectiveDate
		params.RefundAmount = decimal.NullDecimal{Decimal: s.Refund.Amount.Amount(), Valid: true}
	}
	n, err := r.q.UpdatePlan(ctx, params)
	if err != nil {
		return fmt.Errorf("billing: guardando plan: %w", err)
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	return r.writeInstallments(ctx, s.ID, s.Installments)
}

func (r *repository) InsertPayment(ctx context.Context, p app.Payment, key string, allocations []domain.Allocation) error {
	err := r.q.InsertPayment(ctx, billingsqlc.InsertPaymentParams{
		ID: p.ID, TenantID: r.tenant, PlanID: p.PlanID, Amount: p.Amount.Amount(), Surplus: p.Surplus.Amount(), Currency: string(p.Amount.Currency()),
		PaidOn: p.PaidOn, Method: p.Method, ReportedBy: p.ReportedBy, SupportReference: p.SupportReference,
		IdempotencyKey: key, RequestHash: p.RequestHash, CreatedAt: p.CreatedAt, CreatedBy: actor(ctx),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			// Dos envíos simultáneos con la misma llave: el otro ya confirmó; el cliente reintenta y lo recibe.
			return domain.ErrConcurrentUpdate
		}
		return fmt.Errorf("billing: guardando pago: %w", err)
	}
	for _, a := range allocations {
		if err := r.q.InsertAllocation(ctx, billingsqlc.InsertAllocationParams{TenantID: r.tenant, PaymentID: p.ID, InstallmentID: a.InstallmentID, Amount: a.Amount.Amount()}); err != nil {
			return fmt.Errorf("billing: guardando aplicación: %w", err)
		}
	}
	return nil
}

func paymentOf(id, planID uuid.UUID, amount, surplus decimal.Decimal, currency string, paidOn kernel.Date, method, by, reference, hash string, at time.Time, reversed *time.Time) (app.Payment, error) {
	a, err := kernel.NewMoney(amount, kernel.Currency(currency))
	if err != nil {
		return app.Payment{}, err
	}
	s, err := kernel.NewMoney(surplus, kernel.Currency(currency))
	if err != nil {
		return app.Payment{}, err
	}
	return app.Payment{ID: id, PlanID: planID, Amount: a, Surplus: s, PaidOn: paidOn, Method: method, ReportedBy: by, SupportReference: reference, RequestHash: hash, CreatedAt: at, ReversedAt: reversed}, nil
}

func (r *repository) PaymentByKey(ctx context.Context, key string) (app.Payment, bool, error) {
	row, err := r.q.GetPaymentByKey(ctx, billingsqlc.GetPaymentByKeyParams{TenantID: r.tenant, IdempotencyKey: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Payment{}, false, nil
	}
	if err != nil {
		return app.Payment{}, false, fmt.Errorf("billing: buscando llave: %w", err)
	}
	p, err := paymentOf(row.ID, row.PlanID, row.Amount, row.Surplus, row.Currency, row.PaidOn, row.Method, row.ReportedBy, row.SupportReference, row.RequestHash, row.CreatedAt, row.ReversedAt)
	return p, true, err
}

func (r *repository) GetPayment(ctx context.Context, planID, id uuid.UUID) (app.Payment, []domain.Allocation, error) {
	row, err := r.q.GetPayment(ctx, billingsqlc.GetPaymentParams{TenantID: r.tenant, ID: id, PlanID: planID})
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Payment{}, nil, app.ErrPaymentNotFound
	}
	if err != nil {
		return app.Payment{}, nil, fmt.Errorf("billing: leyendo pago: %w", err)
	}
	p, err := paymentOf(row.ID, row.PlanID, row.Amount, row.Surplus, row.Currency, row.PaidOn, row.Method, row.ReportedBy, row.SupportReference, row.RequestHash, row.CreatedAt, row.ReversedAt)
	if err != nil {
		return app.Payment{}, nil, err
	}
	rows, err := r.q.ListAllocations(ctx, billingsqlc.ListAllocationsParams{TenantID: r.tenant, PaymentID: id})
	if err != nil {
		return app.Payment{}, nil, fmt.Errorf("billing: leyendo aplicaciones: %w", err)
	}
	out := make([]domain.Allocation, 0, len(rows))
	for _, a := range rows {
		m, err := kernel.NewMoney(a.Amount, kernel.Currency(row.Currency))
		if err != nil {
			return app.Payment{}, nil, err
		}
		out = append(out, domain.Allocation{InstallmentID: a.InstallmentID, Amount: m})
	}
	return p, out, nil
}

func (r *repository) ListPayments(ctx context.Context, planID uuid.UUID) ([]app.Payment, error) {
	rows, err := r.q.ListPayments(ctx, billingsqlc.ListPaymentsParams{TenantID: r.tenant, PlanID: planID})
	if err != nil {
		return nil, fmt.Errorf("billing: listando pagos: %w", err)
	}
	out := make([]app.Payment, 0, len(rows))
	for _, row := range rows {
		p, err := paymentOf(row.ID, row.PlanID, row.Amount, row.Surplus, row.Currency, row.PaidOn, row.Method, row.ReportedBy, row.SupportReference, row.RequestHash, row.CreatedAt, row.ReversedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (r *repository) MarkReversed(ctx context.Context, id uuid.UUID, reason string, at time.Time) error {
	n, err := r.q.MarkPaymentReversed(ctx, billingsqlc.MarkPaymentReversedParams{TenantID: r.tenant, ID: id, At: &at, Actor: actor(ctx), Reason: pgtype.Text{String: reason, Valid: true}})
	if err != nil {
		return fmt.Errorf("billing: reversando pago: %w", err)
	}
	if n == 0 {
		return domain.ErrPaymentReversed
	}
	return nil
}

func (r *repository) InsertActivity(ctx context.Context, planID uuid.UUID, a app.Activity) error {
	err := r.q.InsertActivity(ctx, billingsqlc.InsertActivityParams{ID: a.ID, TenantID: r.tenant, PlanID: planID, Channel: a.Channel, Note: a.Note, CreatedAt: a.CreatedAt, CreatedBy: actor(ctx)})
	if err != nil {
		return fmt.Errorf("billing: guardando gestión: %w", err)
	}
	return nil
}

func (r *repository) ListActivities(ctx context.Context, planID uuid.UUID) ([]app.Activity, error) {
	rows, err := r.q.ListActivities(ctx, billingsqlc.ListActivitiesParams{TenantID: r.tenant, PlanID: planID})
	if err != nil {
		return nil, fmt.Errorf("billing: listando gestiones: %w", err)
	}
	out := make([]app.Activity, 0, len(rows))
	for _, a := range rows {
		out = append(out, app.Activity{ID: a.ID, Channel: a.Channel, Note: a.Note, CreatedAt: a.CreatedAt})
	}
	return out, nil
}

func (r *repository) GraceDays(ctx context.Context, insurerID uuid.UUID) (int32, error) {
	days, err := r.q.GetGraceDays(ctx, billingsqlc.GetGraceDaysParams{TenantID: r.tenant, InsurerID: uuid.NullUUID{UUID: insurerID, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultGraceDays, nil
	}
	if err != nil {
		return 0, fmt.Errorf("billing: leyendo días de gracia: %w", err)
	}
	return days, nil
}

func (r *repository) ListSettings(ctx context.Context) ([]app.ArrearsSetting, error) {
	rows, err := r.q.ListArrearsSettings(ctx, r.tenant)
	if err != nil {
		return nil, fmt.Errorf("billing: listando días de gracia: %w", err)
	}
	out := make([]app.ArrearsSetting, 0, len(rows))
	for _, s := range rows {
		out = append(out, app.ArrearsSetting{InsurerID: s.InsurerID, GraceDays: s.GraceDays})
	}
	return out, nil
}

func (r *repository) SetSetting(ctx context.Context, s app.ArrearsSetting, at time.Time) error {
	err := r.q.UpsertArrearsSetting(ctx, billingsqlc.UpsertArrearsSettingParams{TenantID: r.tenant, InsurerID: s.InsurerID, GraceDays: s.GraceDays, At: at, Actor: actor(ctx)})
	if err != nil {
		return fmt.Errorf("billing: guardando días de gracia: %w", err)
	}
	return nil
}

func (r *repository) ListReceivables(ctx context.Context, today kernel.Date, minDays, maxDays int32, after uuid.NullUUID, limit int32) ([]app.Receivable, error) {
	rows, err := r.q.ListReceivables(ctx, billingsqlc.ListReceivablesParams{TenantID: r.tenant, Today: today, MinDays: minDays, MaxDays: maxDays, After: after, PageSize: limit})
	if err != nil {
		return nil, fmt.Errorf("billing: listando cartera: %w", err)
	}
	out := make([]app.Receivable, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.Receivable{
			InstallmentID: x.ID, PolicyID: x.PolicyID, PolicyNumber: x.PolicyNumber, InsurerID: x.InsurerID, HolderPartyID: x.HolderPartyID,
			Currency: x.Currency, DueDate: x.DueDate, Balance: x.Amount.Sub(x.Paid), DaysOverdue: x.DaysOverdue,
		})
	}
	return out, nil
}

func (r *repository) Summary(ctx context.Context, today kernel.Date) ([]app.Bucket, error) {
	rows, err := r.q.ReceivablesSummary(ctx, billingsqlc.ReceivablesSummaryParams{TenantID: r.tenant, Today: today})
	if err != nil {
		return nil, fmt.Errorf("billing: resumen de cartera: %w", err)
	}
	out := make([]app.Bucket, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.Bucket{Currency: x.Currency, Bucket: x.Bucket, Balance: x.Balance, Installments: x.Installments})
	}
	return out, nil
}

func (r *repository) PoliciesWithDue(ctx context.Context, today kernel.Date) ([]uuid.UUID, error) {
	ids, err := r.q.PoliciesWithDueInstallments(ctx, billingsqlc.PoliciesWithDueInstallmentsParams{TenantID: r.tenant, Today: today})
	if err != nil {
		return nil, fmt.Errorf("billing: pólizas con cuotas: %w", err)
	}
	return ids, nil
}

func (r *repository) LeadDays(ctx context.Context) (int32, error) {
	days, err := r.q.GetLeadDays(ctx, r.tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultPaymentLeadDays, nil
	}
	if err != nil {
		return 0, fmt.Errorf("billing: leyendo días de anticipación: %w", err)
	}
	return days, nil
}

func (r *repository) SetLeadDays(ctx context.Context, days int32, at time.Time) error {
	if err := r.q.SetLeadDays(ctx, billingsqlc.SetLeadDaysParams{TenantID: r.tenant, LeadDays: days, At: at, Actor: actor(ctx)}); err != nil {
		return fmt.Errorf("billing: guardando días de anticipación: %w", err)
	}
	return nil
}

func (r *repository) InstallmentsDueOn(ctx context.Context, dates []kernel.Date) ([]app.DueInstallment, error) {
	rows, err := r.q.InstallmentsDueOn(ctx, billingsqlc.InstallmentsDueOnParams{TenantID: r.tenant, Dates: dates})
	if err != nil {
		return nil, fmt.Errorf("billing: cuotas por recordar: %w", err)
	}
	out := make([]app.DueInstallment, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.DueInstallment{InstallmentID: x.ID, PolicyID: x.PolicyID, DueDate: x.DueDate})
	}
	return out, nil
}
