// Package postgres implementa la persistencia de comisiones con pgx + sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	commissionssqlc "github.com/marvinrepremarca/seguros/internal/modules/commissions/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/commissions/app"
	"github.com/marvinrepremarca/seguros/internal/modules/commissions/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

const uniqueViolation = "23505"

// Store implementa app.Store.
type Store struct {
	db       *db.DB
	recorder *audit.Recorder
}

// NewStore construye el Store.
func NewStore(database *db.DB, recorder *audit.Recorder) *Store {
	return &Store{db: database, recorder: recorder}
}

// WithinTenant implementa app.Store.
func (s *Store) WithinTenant(ctx context.Context, fn func(ctx context.Context, uow app.UnitOfWork) error) error {
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		repo, err := s.Bind(ctx, tx)
		if err != nil {
			return err
		}
		return fn(ctx, &unitOfWork{tx: tx, recorder: s.recorder, repo: repo})
	})
}

// Bind da el repositorio sobre la transacción de un consumidor, que incluye el inbox.
func (s *Store) Bind(ctx context.Context, tx pgx.Tx) (app.Repository, error) {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return nil, db.ErrNoTenant
	}
	return &repository{q: commissionssqlc.New(tx), tenant: uuid.UUID(tenant)}, nil
}

type unitOfWork struct {
	tx       pgx.Tx
	recorder *audit.Recorder
	repo     app.Repository
}

func (u *unitOfWork) Commissions() app.Repository { return u.repo }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.recorder.Record(ctx, u.tx, e)
}

type repository struct {
	q      *commissionssqlc.Queries
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
		return fmt.Errorf("commissions: %s: %w", op, err)
	}
	return nil
}

func newID() (uuid.UUID, error) { return uuid.NewV7() }

func (r *repository) UpsertTerms(ctx context.Context, t app.Terms) error {
	return wrap(r.q.UpsertTerms(ctx, commissionssqlc.UpsertTermsParams{
		TenantID: r.tenant, PolicyID: t.PolicyID, PolicyNumber: t.PolicyNumber, InsurerID: t.InsurerID, LineID: t.LineID, ProductID: t.ProductID,
		Currency: string(t.Net.Currency()), NetPremium: t.Net.Amount(), Taxes: t.Taxes.Amount(),
	}), "guardando póliza")
}

func (r *repository) GetTerms(ctx context.Context, policyID uuid.UUID) (app.Terms, bool, error) {
	row, err := r.q.GetTerms(ctx, commissionssqlc.GetTermsParams{TenantID: r.tenant, PolicyID: policyID})
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Terms{}, false, nil
	}
	if err != nil {
		return app.Terms{}, false, wrap(err, "leyendo póliza")
	}
	net, err := kernel.NewMoney(row.NetPremium, kernel.Currency(row.Currency))
	if err != nil {
		return app.Terms{}, false, err
	}
	taxes, err := kernel.NewMoney(row.Taxes, kernel.Currency(row.Currency))
	if err != nil {
		return app.Terms{}, false, err
	}
	return app.Terms{
		PolicyID: row.PolicyID, InsurerID: row.InsurerID, LineID: row.LineID, ProductID: row.ProductID, PolicyNumber: row.PolicyNumber, Net: net, Taxes: taxes,
	}, true, nil
}

func (r *repository) FindPolicy(ctx context.Context, insurer uuid.UUID, number string) (uuid.NullUUID, error) {
	id, err := r.q.FindPolicyByNumber(ctx, commissionssqlc.FindPolicyByNumberParams{TenantID: r.tenant, InsurerID: insurer, PolicyNumber: number})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.NullUUID{}, nil
	}
	if err != nil {
		return uuid.NullUUID{}, wrap(err, "buscando póliza")
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

func (r *repository) InsertAgent(ctx context.Context, policyID uuid.UUID, agent uuid.NullUUID, on kernel.Date) error {
	id, err := newID()
	if err != nil {
		return err
	}
	return wrap(r.q.InsertAgent(ctx, commissionssqlc.InsertAgentParams{ID: id, TenantID: r.tenant, PolicyID: policyID, AgentID: agent, EffectiveOn: on}), "guardando asesor")
}

func (r *repository) AgentOn(ctx context.Context, policyID uuid.UUID, on kernel.Date) (uuid.NullUUID, error) {
	agent, err := r.q.AgentOn(ctx, commissionssqlc.AgentOnParams{TenantID: r.tenant, PolicyID: policyID, OnDate: on})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.NullUUID{}, nil
	}
	if err != nil {
		return uuid.NullUUID{}, wrap(err, "buscando asesor")
	}
	return agent, nil
}

func (r *repository) InsertAccrual(ctx context.Context, x *domain.Accrual, at time.Time) error {
	a := x.Snapshot()
	_, err := r.q.InsertAccrual(ctx, commissionssqlc.InsertAccrualParams{
		ID: a.ID, TenantID: r.tenant, PolicyID: a.PolicyID, Kind: string(a.Kind), SourceID: a.SourceID, Base: a.Base.Amount(), Percentage: a.Percentage.Decimal(),
		Amount: a.Amount.Amount(), Currency: string(a.Amount.Currency()), AgentID: a.AgentID, AccruedOn: a.AccruedOn, MissingAgreement: a.MissingAgreement, CreatedAt: at,
	})
	return wrap(err, "guardando causación")
}

func accrualOf(s domain.AccrualSnapshot, base, amount, pct decimal.Decimal, currency string) (*domain.Accrual, error) {
	var err error
	if s.Base, err = kernel.NewMoney(base, kernel.Currency(currency)); err != nil {
		return nil, err
	}
	if s.Amount, err = kernel.NewMoney(amount, kernel.Currency(currency)); err != nil {
		return nil, err
	}
	if s.Percentage, err = kernel.NewPercentage(pct); err != nil {
		return nil, err
	}
	return domain.RehydrateAccrual(s), nil
}

func (r *repository) AccrualBySource(ctx context.Context, kind domain.Kind, source uuid.UUID) (*domain.Accrual, bool, error) {
	x, err := r.q.GetAccrualBySource(ctx, commissionssqlc.GetAccrualBySourceParams{TenantID: r.tenant, Kind: string(kind), SourceID: source})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrap(err, "leyendo causación")
	}
	a, err := accrualOf(domain.AccrualSnapshot{
		ID: x.ID, PolicyID: x.PolicyID, SourceID: x.SourceID, Kind: kind, AgentID: x.AgentID, AccruedOn: x.AccruedOn, MissingAgreement: x.MissingAgreement, StatementID: x.StatementID,
	}, x.Base, x.Amount, x.Percentage, x.Currency)
	return a, err == nil, err
}

func (r *repository) ListAccruals(ctx context.Context, onlyPending bool, after uuid.NullUUID, limit int32) ([]app.Accrual, error) {
	rows, err := r.q.ListAccruals(ctx, commissionssqlc.ListAccrualsParams{TenantID: r.tenant, OnlyPending: onlyPending, After: after, PageSize: limit})
	if err != nil {
		return nil, wrap(err, "listando causaciones")
	}
	out := make([]app.Accrual, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.Accrual{
			ID: x.ID, PolicyID: x.PolicyID, PolicyNumber: x.PolicyNumber, Kind: domain.Kind(x.Kind), Base: x.Base, Percentage: x.Percentage, Amount: x.Amount,
			Currency: x.Currency, AgentID: x.AgentID, AccruedOn: x.AccruedOn, MissingAgreement: x.MissingAgreement, Reconciled: x.StatementID.Valid,
		})
	}
	return out, nil
}

func (r *repository) PendingAccruals(ctx context.Context, policyID uuid.UUID) ([]app.PendingAccrual, error) {
	rows, err := r.q.PendingAccruals(ctx, commissionssqlc.PendingAccrualsParams{TenantID: r.tenant, PolicyID: policyID})
	if err != nil {
		return nil, wrap(err, "leyendo pendientes")
	}
	out := make([]app.PendingAccrual, 0, len(rows))
	for _, x := range rows {
		a, err := accrualOf(domain.AccrualSnapshot{
			ID: x.ID, PolicyID: policyID, SourceID: x.SourceID, Kind: domain.Kind(x.Kind), AgentID: x.AgentID, AccruedOn: x.AccruedOn, MissingAgreement: x.MissingAgreement,
		}, x.Base, x.Amount, x.Percentage, x.Currency)
		if err != nil {
			return nil, err
		}
		out = append(out, app.PendingAccrual{Accrual: a, LineID: x.LineID})
	}
	return out, nil
}

func (r *repository) MarkReconciled(ctx context.Context, ids []uuid.UUID, statementID uuid.UUID) error {
	return wrap(r.q.MarkAccrualsReconciled(ctx, commissionssqlc.MarkAccrualsReconciledParams{
		TenantID: r.tenant, Ids: ids, StatementID: uuid.NullUUID{UUID: statementID, Valid: true},
	}), "conciliando")
}

func (r *repository) InsertStatement(ctx context.Context, id, insurer uuid.UUID, reference string, on kernel.Date, at time.Time) error {
	err := r.q.InsertStatement(ctx, commissionssqlc.InsertStatementParams{ID: id, TenantID: r.tenant, InsurerID: insurer, Reference: reference, ReceivedOn: on, CreatedAt: at, CreatedBy: actor(ctx)})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return app.ErrStatementExists
	}
	return wrap(err, "guardando liquidación")
}

func (r *repository) CloseDiscrepancies(ctx context.Context, insurer uuid.UUID, number string, at time.Time) error {
	return wrap(r.q.CloseDiscrepancies(ctx, commissionssqlc.CloseDiscrepanciesParams{At: &at, TenantID: r.tenant, InsurerID: insurer, PolicyNumber: number}), "cerrando diferencias")
}

func (r *repository) InsertLine(ctx context.Context, l app.Line) error {
	id, err := newID()
	if err != nil {
		return err
	}
	return wrap(r.q.InsertLine(ctx, commissionssqlc.InsertLineParams{
		ID: id, TenantID: r.tenant, StatementID: l.StatementID, PolicyNumber: l.PolicyNumber, PolicyID: l.PolicyID, Reported: l.Reported, Expected: l.Expected, Matched: l.Matched,
	}), "guardando línea")
}

func (r *repository) ListDiscrepancies(ctx context.Context, after uuid.NullUUID, limit int32) ([]app.Discrepancy, error) {
	rows, err := r.q.ListDiscrepancies(ctx, commissionssqlc.ListDiscrepanciesParams{TenantID: r.tenant, After: after, PageSize: limit})
	if err != nil {
		return nil, wrap(err, "listando diferencias")
	}
	out := make([]app.Discrepancy, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.Discrepancy{ID: x.ID, InsurerID: x.InsurerID, StatementReference: x.Reference, PolicyNumber: x.PolicyNumber, PolicyID: x.PolicyID, Reported: x.Reported, Expected: x.Expected})
	}
	return out, nil
}

func (r *repository) RateFor(ctx context.Context, agent, line uuid.UUID, on kernel.Date) (decimal.Decimal, bool, error) {
	rate, err := r.q.RateFor(ctx, commissionssqlc.RateForParams{TenantID: r.tenant, AgentID: agent, LineID: uuid.NullUUID{UUID: line, Valid: true}, OnDate: on})
	if errors.Is(err, pgx.ErrNoRows) {
		return decimal.Zero, false, nil
	}
	if err != nil {
		return decimal.Zero, false, wrap(err, "buscando tasa")
	}
	return rate, true, nil
}

func (r *repository) InsertAgentCommission(ctx context.Context, c app.AgentCommission, at time.Time) error {
	return wrap(r.q.InsertAgentCommission(ctx, commissionssqlc.InsertAgentCommissionParams{
		ID: c.ID, TenantID: r.tenant, AccrualID: c.AccrualID, AgentID: c.AgentID, Percentage: c.Percentage, Amount: c.Amount, Currency: c.Currency, MissingRate: c.MissingRate, CreatedAt: at,
	}), "guardando comisión interna")
}

func (r *repository) ListAgentCommissions(ctx context.Context, agent, after uuid.NullUUID, limit int32) ([]app.AgentCommission, error) {
	rows, err := r.q.ListAgentCommissions(ctx, commissionssqlc.ListAgentCommissionsParams{TenantID: r.tenant, AgentID: agent, After: after, PageSize: limit})
	if err != nil {
		return nil, wrap(err, "listando comisión interna")
	}
	out := make([]app.AgentCommission, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.AgentCommission{ID: x.ID, AccrualID: x.AccrualID, AgentID: x.AgentID, Percentage: x.Percentage, Amount: x.Amount, Currency: x.Currency, MissingRate: x.MissingRate})
	}
	return out, nil
}

func (r *repository) InsertRate(ctx context.Context, x app.AgentRate, at time.Time) error {
	return wrap(r.q.InsertRate(ctx, commissionssqlc.InsertRateParams{
		ID: x.ID, TenantID: r.tenant, AgentID: x.AgentID, LineID: x.LineID, Percentage: x.Percentage, ValidFrom: x.From, ValidTo: x.To, CreatedAt: at, CreatedBy: actor(ctx),
	}), "guardando tasa")
}

func (r *repository) ListRates(ctx context.Context) ([]app.AgentRate, error) {
	rows, err := r.q.ListRates(ctx, r.tenant)
	if err != nil {
		return nil, wrap(err, "listando tasas")
	}
	out := make([]app.AgentRate, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.AgentRate{ID: x.ID, AgentID: x.AgentID, LineID: x.LineID, Percentage: x.Percentage, From: x.ValidFrom, To: x.ValidTo})
	}
	return out, nil
}

func (r *repository) Tolerance(ctx context.Context, currency kernel.Currency) (kernel.Money, error) {
	v, err := r.q.GetTolerance(ctx, commissionssqlc.GetToleranceParams{TenantID: r.tenant, Currency: string(currency)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultTolerance(currency)
	}
	if err != nil {
		return kernel.Money{}, wrap(err, "leyendo tolerancia")
	}
	return kernel.NewMoney(v, currency)
}

func (r *repository) ListTolerances(ctx context.Context) ([]kernel.Money, error) {
	rows, err := r.q.ListTolerances(ctx, r.tenant)
	if err != nil {
		return nil, wrap(err, "listando tolerancias")
	}
	out := make([]kernel.Money, 0, len(rows))
	for _, x := range rows {
		m, err := kernel.NewMoney(x.Tolerance, kernel.Currency(x.Currency))
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (r *repository) SetTolerance(ctx context.Context, v kernel.Money, at time.Time) error {
	return wrap(r.q.SetTolerance(ctx, commissionssqlc.SetToleranceParams{TenantID: r.tenant, Currency: string(v.Currency()), Tolerance: v.Amount(), At: at, Actor: actor(ctx)}), "guardando tolerancia")
}
