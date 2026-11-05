package app

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/commissions/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// StatementInput es una liquidación de la aseguradora.
type StatementInput struct {
	InsurerID  uuid.UUID
	Reference  string
	ReceivedOn kernel.Date
	Lines      []StatementLine
}

// StatementLine es lo liquidado por una póliza.
type StatementLine struct {
	PolicyNumber string
	Amount       decimal.Decimal
}

// StatementResult resume la conciliación.
type StatementResult struct {
	ID                     uuid.UUID
	Matched, Discrepancies int32
}

// RegisterStatement concilia una liquidación contra lo causado sin conciliar de cada póliza. Dentro de
// la tolerancia, las causaciones quedan conciliadas y generan la comisión interna (DECISIONS N5, T4); si
// no, queda la diferencia. Una línea posterior de la misma póliza reemplaza la diferencia.
func (s *Service) RegisterStatement(ctx context.Context, in StatementInput) (StatementResult, error) {
	if err := authz.Require(ctx, authz.CommissionStatementManage); err != nil {
		return StatementResult{}, err
	}
	id, err := newID()
	if err != nil {
		return StatementResult{}, err
	}
	out := StatementResult{ID: id}
	now := s.Clock.Now().UTC()
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		repo := uow.Commissions()
		if err := repo.InsertStatement(ctx, id, in.InsurerID, in.Reference, in.ReceivedOn, now); err != nil {
			return err
		}
		for _, l := range in.Lines {
			matched, err := reconcileLine(ctx, repo, id, in.InsurerID, l, now)
			if err != nil {
				return err
			}
			if matched {
				out.Matched++
			} else {
				out.Discrepancies++
			}
		}
		return uow.Audit(ctx, audit.Entry{
			Action: "commissions.statement.registered", ResourceType: "commissions.statement", ResourceID: id.String(), Outcome: audit.OutcomeSuccess,
			Details: map[string]string{"matched": fmt.Sprint(out.Matched), "discrepancies": fmt.Sprint(out.Discrepancies)},
		})
	})
	return out, err
}

func reconcileLine(ctx context.Context, repo Repository, statement, insurer uuid.UUID, l StatementLine, now time.Time) (bool, error) {
	if err := repo.CloseDiscrepancies(ctx, insurer, l.PolicyNumber, now); err != nil {
		return false, err
	}
	policy, err := repo.FindPolicy(ctx, insurer, l.PolicyNumber)
	if err != nil {
		return false, err
	}
	line := Line{StatementID: statement, PolicyNumber: l.PolicyNumber, PolicyID: policy, Reported: l.Amount, Expected: decimal.Zero}
	if !policy.Valid {
		return false, repo.InsertLine(ctx, line)
	}
	terms, _, err := repo.GetTerms(ctx, policy.UUID)
	if err != nil {
		return false, err
	}
	reported, err := kernel.NewMoney(l.Amount, terms.Net.Currency())
	if err != nil {
		return false, err
	}
	tolerance, err := repo.Tolerance(ctx, terms.Net.Currency())
	if err != nil {
		return false, err
	}
	pending, err := repo.PendingAccruals(ctx, policy.UUID)
	if err != nil {
		return false, err
	}
	accruals := make([]*domain.Accrual, 0, len(pending))
	for _, a := range pending {
		accruals = append(accruals, a.Accrual)
	}
	matched, expected, err := domain.Reconcile(accruals, statement, reported, tolerance)
	if err != nil {
		return false, err
	}
	line.Expected, line.Matched = expected.Amount(), matched
	if err := repo.InsertLine(ctx, line); err != nil {
		return false, err
	}
	if !matched {
		return false, nil
	}
	ids := make([]uuid.UUID, 0, len(accruals))
	for _, a := range accruals {
		ids = append(ids, a.Snapshot().ID)
	}
	if err := repo.MarkReconciled(ctx, ids, statement); err != nil {
		return false, err
	}
	for _, a := range pending {
		if err := accrueAgent(ctx, repo, a, now); err != nil {
			return false, err
		}
	}
	return true, nil
}

// accrueAgent genera la comisión interna de una causación conciliada. Sin tasa, 0 % con alerta (N5).
func accrueAgent(ctx context.Context, repo Repository, p PendingAccrual, now time.Time) error {
	a := p.Accrual.Snapshot()
	if !a.AgentID.Valid {
		return nil
	}
	rate, found, err := repo.RateFor(ctx, a.AgentID.UUID, p.LineID, a.AccruedOn)
	if err != nil {
		return err
	}
	pct, err := kernel.NewPercentage(rate)
	if err != nil {
		return err
	}
	amount, err := domain.Share(a.Amount, pct)
	if err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	return repo.InsertAgentCommission(ctx, AgentCommission{
		ID: id, AccrualID: a.ID, AgentID: a.AgentID.UUID, Percentage: rate, Amount: amount.Amount(), Currency: string(a.Amount.Currency()), MissingRate: !found,
	}, now)
}
