package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// OnPolicyIssued abre el caso y, si la póliza renueva otra, cierra el caso anterior como renovado.
func OnPolicyIssued(ctx context.Context, uow UnitOfWork, clock kernel.Clock, ev policies.PolicyIssued) error {
	to, err := kernel.ParseDate(ev.EffectiveTo)
	if err != nil {
		return err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	now := clock.Now().UTC()
	err = uow.Renewals().InsertCase(ctx, domain.Snapshot{
		ID: id, PolicyID: ev.PolicyID, PolicyNumber: ev.PolicyNumber, InsurerID: ev.InsurerID, HolderPartyID: ev.HolderPartyID, EffectiveTo: to,
	}, now)
	if err != nil || ev.RenewedFromPolicyID == nil {
		return err
	}
	prev, err := uow.Renewals().GetCase(ctx, *ev.RenewedFromPolicyID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := prev.MarkRenewed(ev.PolicyID); err != nil {
		if errors.Is(err, domain.ErrInvalidTransition) {
			return nil // ya renovado o cerrado: no aplica
		}
		return err
	}
	return uow.Renewals().SaveCase(ctx, prev, now)
}

// OnPolicyCancelled cierra el caso.
func OnPolicyCancelled(ctx context.Context, uow UnitOfWork, clock kernel.Clock, ev policies.PolicyCancelled) error {
	c, err := uow.Renewals().GetCase(ctx, ev.PolicyID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	c.Close()
	return uow.Renewals().SaveCase(ctx, c, clock.Now().UTC())
}

// OnPolicyExpired repite el aviso de pérdida: antes de vencer, Policies no podía cerrar la póliza.
func OnPolicyExpired(ctx context.Context, uow UnitOfWork, _ kernel.Clock, ev policies.PolicyExpired) error {
	c, err := uow.Renewals().GetCase(ctx, ev.PolicyID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if c.Snapshot().Status != domain.StatusLost {
		return nil
	}
	return uow.Publish(ctx, contracts.TopicRenewalLost, contracts.RenewalLost{PolicyID: ev.PolicyID})
}
