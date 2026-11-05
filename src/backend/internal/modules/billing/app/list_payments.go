package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// ListPayments lista los pagos del plan.
func (s *Service) ListPayments(ctx context.Context, policyID uuid.UUID) ([]Payment, error) {
	if err := authz.Require(ctx, authz.BillingPlanRead); err != nil {
		return nil, err
	}
	var out []Payment
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := uow.Billing().GetPlan(ctx, policyID)
		if err != nil {
			return err
		}
		out, err = uow.Billing().ListPayments(ctx, p.Snapshot().ID)
		return err
	})
	return out, err
}
