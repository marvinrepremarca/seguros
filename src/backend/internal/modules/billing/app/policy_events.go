package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/billing/domain"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// OnPolicyIssued crea el plan de pagos.
func OnPolicyIssued(ctx context.Context, uow UnitOfWork, ev policies.PolicyIssued) error {
	currency := kernel.Currency(ev.Currency)
	net, err := kernel.ParseMoney(ev.NetPremium, currency)
	if err != nil {
		return err
	}
	taxes, err := kernel.ParseMoney(ev.Taxes, currency)
	if err != nil {
		return err
	}
	total, err := net.Add(taxes)
	if err != nil {
		return err
	}
	from, err := kernel.ParseDate(ev.EffectiveFrom)
	if err != nil {
		return err
	}
	to, err := kernel.ParseDate(ev.EffectiveTo)
	if err != nil {
		return err
	}
	id, err := NewID()
	if err != nil {
		return err
	}
	p, err := domain.NewPaymentPlan(domain.PlanInput{
		ID: id, PolicyID: ev.PolicyID, PolicyNumber: ev.PolicyNumber, InsurerID: ev.InsurerID, HolderPartyID: ev.HolderPartyID,
		PaymentForm: ev.PaymentForm, Total: total, From: from, To: to, NewID: NewID,
	})
	if err != nil {
		return err
	}
	if err := uow.Billing().InsertPlan(ctx, p); err != nil {
		return err
	}
	return uow.Audit(ctx, entry("billing.plan.created", id, nil))
}

func planOrNil(ctx context.Context, uow UnitOfWork, policyID uuid.UUID) (*domain.PaymentPlan, error) {
	p, err := uow.Billing().GetPlan(ctx, policyID)
	if errors.Is(err, ErrPlanNotFound) {
		return nil, nil //nolint:nilnil // "no aplica" no es un error en un consumidor
	}
	return p, err
}

// OnPolicyEndorsed reparte la variación de prima (DECISIONS N18).
func (r Reactor) OnPolicyEndorsed(ctx context.Context, uow UnitOfWork, ev policies.PolicyEndorsed) error {
	p, err := planOrNil(ctx, uow, ev.PolicyID)
	if err != nil || p == nil {
		return err
	}
	currency := kernel.Currency(ev.Currency)
	net, err := kernel.ParseMoney(ev.NetPremiumDelta, currency)
	if err != nil {
		return err
	}
	taxes, err := kernel.ParseMoney(ev.TaxesDelta, currency)
	if err != nil {
		return err
	}
	delta, err := net.Add(taxes)
	if err != nil {
		return err
	}
	effective, err := kernel.ParseDate(ev.EffectiveDate)
	if err != nil {
		return err
	}
	today := kernel.Today(r.Clock)
	grace, err := uow.Billing().GraceDays(ctx, p.Snapshot().InsurerID)
	if err != nil {
		return err
	}
	was := p.InArrears(today, int(grace))
	if err := p.ApplyEndorsement(delta, effective, NewID); err != nil {
		return err
	}
	if err := uow.Billing().SavePlan(ctx, p); err != nil {
		return err
	}
	if err := publishCleared(ctx, uow, p, ev.PolicyID, was, today, grace); err != nil {
		return err
	}
	return uow.Audit(ctx, entry("billing.plan.endorsed", p.Snapshot().ID, map[string]string{"endorsement": ev.EndorsementID.String()}))
}

// OnPolicyCancelled anula lo pendiente y registra la devolución informada (DECISIONS N20).
func OnPolicyCancelled(ctx context.Context, uow UnitOfWork, ev policies.PolicyCancelled) error {
	p, err := planOrNil(ctx, uow, ev.PolicyID)
	if err != nil || p == nil {
		return err
	}
	unearned, err := kernel.ParseMoney(ev.UnearnedPremium, kernel.Currency(ev.Currency))
	if err != nil {
		return err
	}
	effective, err := kernel.ParseDate(ev.EffectiveDate)
	if err != nil {
		return err
	}
	if err := p.Cancel(effective, unearned); err != nil {
		return err
	}
	if err := uow.Billing().SavePlan(ctx, p); err != nil {
		return err
	}
	return uow.Audit(ctx, entry("billing.plan.cancelled", p.Snapshot().ID, nil))
}

// Reactor agrupa las reacciones que necesitan la fecha de negocio.
type Reactor struct{ Clock kernel.Clock }
