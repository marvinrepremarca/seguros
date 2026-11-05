package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/billing/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/billing/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ScheduleInput es una cuota del ajuste.
type ScheduleInput struct {
	DueDate kernel.Date
	Amount  decimal.Decimal
}

// AdjustSchedule aplica el calendario informado por la aseguradora (DECISIONS N15).
func (s *Service) AdjustSchedule(ctx context.Context, policyID uuid.UUID, version int32, support string, in []ScheduleInput) (PlanView, error) {
	if err := authz.Require(ctx, authz.BillingPlanAdjust); err != nil {
		return PlanView{}, err
	}
	support, err := cleanText(support, 200)
	if err != nil {
		return PlanView{}, err
	}
	var out PlanView
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := uow.Billing().GetPlan(ctx, policyID)
		if err != nil {
			return err
		}
		snap := p.Snapshot()
		if snap.Version != version {
			return domain.ErrConcurrentUpdate
		}
		grace, err := uow.Billing().GraceDays(ctx, snap.InsurerID)
		if err != nil {
			return err
		}
		wasInArrears := p.InArrears(s.today(), int(grace))
		schedule := make([]domain.ScheduledInstallment, 0, len(in))
		for _, i := range in {
			m, err := kernel.NewMoney(i.Amount, snap.Total.Currency())
			if err != nil {
				return domain.ErrInvalidInstallment
			}
			schedule = append(schedule, domain.ScheduledInstallment{DueDate: i.DueDate, Amount: m})
		}
		if err := p.AdjustSchedule(schedule, NewID); err != nil {
			return err
		}
		if err := uow.Billing().SavePlan(ctx, p); err != nil {
			return err
		}
		if err := uow.Audit(ctx, entry("billing.plan.schedule_adjusted", snap.ID, map[string]string{"installments_before": fmt.Sprint(len(snap.Installments)), "installments_after": fmt.Sprint(len(p.Snapshot().Installments)), "support": support})); err != nil {
			return err
		}
		if err := publishCleared(ctx, uow, p, policyID, wasInArrears, s.today(), grace); err != nil {
			return err
		}
		// SavePlan ya aumentó la versión en la BD.
		if out, err = s.load(ctx, uow, policyID); err != nil {
			return err
		}
		return nil
	})
	return out, err
}

func publishCleared(ctx context.Context, uow UnitOfWork, p *domain.PaymentPlan, policyID uuid.UUID, was bool, today kernel.Date, grace int32) error {
	if !was || p.InArrears(today, int(grace)) {
		return nil
	}
	return uow.Publish(ctx, contracts.TopicArrearsCleared, contracts.ArrearsCleared{PolicyID: policyID})
}
