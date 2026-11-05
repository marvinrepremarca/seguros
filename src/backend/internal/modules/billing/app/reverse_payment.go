package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/billing/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/billing/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// ReversePayment reversa un pago: es la única corrección posible (pagos inmutables, DECISIONS N6).
func (s *Service) ReversePayment(ctx context.Context, policyID, paymentID uuid.UUID, reason string) error {
	if err := authz.Require(ctx, authz.BillingPaymentReverse); err != nil {
		return err
	}
	reason, err := cleanText(reason, 500)
	if err != nil {
		return err
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		repo := uow.Billing()
		p, err := repo.GetPlan(ctx, policyID)
		if err != nil {
			return err
		}
		snap := p.Snapshot()
		pay, allocations, err := repo.GetPayment(ctx, snap.ID, paymentID)
		if err != nil {
			return err
		}
		if pay.ReversedAt != nil {
			return domain.ErrPaymentReversed
		}
		if err := p.ReversePayment(allocations, pay.Surplus); err != nil {
			return err
		}
		if err := repo.SavePlan(ctx, p); err != nil {
			return err
		}
		if err := repo.MarkReversed(ctx, paymentID, reason, s.Clock.Now().UTC()); err != nil {
			return err
		}
		toInstallments, err := applied(pay.Amount, pay.Surplus)
		if err != nil {
			return err
		}
		// El motivo es texto libre: se queda en el pago, no en la bitácora ni en el evento.
		if err := uow.Audit(ctx, entry("billing.payment.reversed", snap.ID, map[string]string{"payment": paymentID.String()})); err != nil {
			return err
		}
		return uow.Publish(ctx, contracts.TopicPaymentReversed, contracts.PaymentReversed{
			PolicyID: policyID, PaymentID: paymentID, Currency: string(pay.Amount.Currency()), Amount: toInstallments.Amount().String(),
		})
	})
}
