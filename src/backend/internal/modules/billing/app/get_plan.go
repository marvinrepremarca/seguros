package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/billing/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// PlanView es el plan con su estado de mora.
type PlanView struct {
	domain.PlanSnapshot
	InArrears bool
}

// GetPlan consulta el plan de una póliza.
func (s *Service) GetPlan(ctx context.Context, policyID uuid.UUID) (PlanView, error) {
	if err := authz.Require(ctx, authz.BillingPlanRead); err != nil {
		return PlanView{}, err
	}
	var out PlanView
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		out, err = s.load(ctx, uow, policyID)
		return err
	})
	return out, err
}

func (s *Service) load(ctx context.Context, uow UnitOfWork, policyID uuid.UUID) (PlanView, error) {
	p, err := uow.Billing().GetPlan(ctx, policyID)
	if err != nil {
		return PlanView{}, err
	}
	return s.viewOf(ctx, uow, p)
}

func (s *Service) viewOf(ctx context.Context, uow UnitOfWork, p *domain.PaymentPlan) (PlanView, error) {
	snap := p.Snapshot()
	grace, err := uow.Billing().GraceDays(ctx, snap.InsurerID)
	if err != nil {
		return PlanView{}, err
	}
	return PlanView{PlanSnapshot: snap, InArrears: p.InArrears(s.today(), int(grace))}, nil
}
