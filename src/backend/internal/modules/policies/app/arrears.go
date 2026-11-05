package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// MarkInArrears pasa a EnMora una póliza vigente con una cuota fuera de los días de gracia (DECISIONS
// N2). En otro estado no aplica: el evento puede llegar después de cancelarla o de que ya esté en mora.
func MarkInArrears(ctx context.Context, uow UnitOfWork, clock kernel.Clock, policyID uuid.UUID) error {
	p, err := uow.Policies().Get(ctx, domain.PolicyID(policyID))
	if err != nil {
		return err
	}
	from := p.Status()
	if from != domain.StatusActive {
		return nil
	}
	if err := p.MarkInArrears(clock.Now()); err != nil {
		return err
	}
	if err := uow.Policies().Save(ctx, p); err != nil {
		return err
	}
	return record(ctx, uow, p, "policies.policy.fell_into_arrears", from, nil)
}

// ReinstateAfterPayment devuelve a Vigente una póliza en mora sin cuotas fuera de los días de gracia
// (DECISIONS N19). En otro estado no aplica.
func ReinstateAfterPayment(ctx context.Context, uow UnitOfWork, clock kernel.Clock, policyID uuid.UUID) error {
	p, err := uow.Policies().Get(ctx, domain.PolicyID(policyID))
	if err != nil {
		return err
	}
	from := p.Status()
	if from != domain.StatusInArrears {
		return nil
	}
	if err := p.Reinstate(clock.Now()); err != nil {
		return err
	}
	if err := uow.Policies().Save(ctx, p); err != nil {
		return err
	}
	return record(ctx, uow, p, "policies.policy.reinstated", from, nil)
}
