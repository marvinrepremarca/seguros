package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/billing/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/billing/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// PaymentInput son los datos de un pago informado.
type PaymentInput struct {
	Key              string
	Amount           decimal.Decimal
	PaidOn           kernel.Date
	Method           string
	ReportedBy       string
	SupportReference string
}

func (in PaymentInput) hash(policyID uuid.UUID) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s|%s|%s|%s|%s|%s", policyID, in.Amount.String(), in.PaidOn, in.Method, in.ReportedBy, in.SupportReference))
	return hex.EncodeToString(sum[:])
}

func applied(amount, surplus kernel.Money) (kernel.Money, error) { return amount.Sub(surplus) }

// RegisterPayment aplica un pago informado (DECISIONS N1, N6). La misma llave con los mismos datos
// devuelve el pago ya registrado; con otros datos, un conflicto.
func (s *Service) RegisterPayment(ctx context.Context, policyID uuid.UUID, in PaymentInput) (Payment, error) {
	if err := authz.Require(ctx, authz.BillingPaymentRegister); err != nil {
		return Payment{}, err
	}
	reference, err := cleanText(in.SupportReference, 200)
	if err != nil {
		return Payment{}, err
	}
	in.SupportReference = reference
	hash := in.hash(policyID)
	var out Payment
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		repo := uow.Billing()
		if prev, found, err := repo.PaymentByKey(ctx, in.Key); err != nil {
			return err
		} else if found {
			if prev.RequestHash != hash {
				return ErrKeyReused
			}
			out = prev
			return nil
		}
		p, err := repo.GetPlan(ctx, policyID)
		if err != nil {
			return err
		}
		lead, err := repo.LeadDays(ctx)
		if err != nil {
			return err
		}
		if err := p.ValidatePaymentDate(in.PaidOn, s.today(), int(lead)); err != nil {
			return err
		}
		snap := p.Snapshot()
		amount, err := kernel.NewMoney(in.Amount, snap.Total.Currency())
		if err != nil {
			return domain.ErrInvalidPayment
		}
		allocations, surplus, err := p.ApplyPayment(amount)
		if err != nil {
			return err
		}
		id, err := NewID()
		if err != nil {
			return err
		}
		out = Payment{
			ID: id, PlanID: snap.ID, Amount: amount, Surplus: surplus, PaidOn: in.PaidOn, Method: in.Method, ReportedBy: in.ReportedBy,
			SupportReference: in.SupportReference, RequestHash: hash, CreatedAt: s.Clock.Now().UTC(),
		}
		if err := repo.SavePlan(ctx, p); err != nil {
			return err
		}
		if err := repo.InsertPayment(ctx, out, in.Key, allocations); err != nil {
			return err
		}
		grace, err := repo.GraceDays(ctx, snap.InsurerID)
		if err != nil {
			return err
		}
		toInstallments, err := applied(amount, surplus)
		if err != nil {
			return err
		}
		if err := uow.Audit(ctx, entry("billing.payment.registered", snap.ID, map[string]string{"payment": id.String(), "amount": amount.Amount().String(), "currency": string(amount.Currency())})); err != nil {
			return err
		}
		return uow.Publish(ctx, contracts.TopicPaymentApplied, contracts.PaymentApplied{
			PolicyID: policyID, PaymentID: id, Currency: string(amount.Currency()), Amount: toInstallments.Amount().String(),
			PaidOn: in.PaidOn.String(), InArrears: p.InArrears(s.today(), int(grace)),
		})
	})
	return out, err
}
