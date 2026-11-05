package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	billing "github.com/marvinrepremarca/seguros/internal/modules/billing/contracts"
	catalog "github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/commissions/domain"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Reactor causa comisión a partir de los eventos. Lee los acuerdos desde catalog.
type Reactor struct {
	Catalog catalog.Directory
	Clock   kernel.Clock
}

func nullable(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

func (r *Reactor) agreement(ctx context.Context, t Terms, on kernel.Date) (catalog.Agreement, bool, error) {
	return r.Catalog.AgreementFor(ctx, catalog.PolicyRef{InsurerID: t.InsurerID, LineOfBusinessID: t.LineID, ProductID: t.ProductID}, on)
}

// accrue registra base × %; sin acuerdo queda en 0 % con alerta para que la conciliación lo muestre.
func (r *Reactor) accrue(ctx context.Context, repo Repository, t Terms, kind domain.Kind, source uuid.UUID, base kernel.Money, agreement catalog.Agreement, found bool, on kernel.Date) error {
	agent, err := repo.AgentOn(ctx, t.PolicyID, on)
	if err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	zero, err := kernel.NewPercentage(decimal.Zero)
	if err != nil {
		return err
	}
	in := domain.AccrualInput{ID: id, PolicyID: t.PolicyID, SourceID: source, Kind: kind, Base: base, AgentID: agent, On: on}
	if found {
		in.Percentage = &agreement.Percentage
	}
	a, err := domain.NewAccrual(in, zero)
	if err != nil {
		return err
	}
	return repo.InsertAccrual(ctx, a, r.Clock.Now().UTC())
}

// OnPolicyIssued crea la proyección y, con base prima emitida, causa sobre la prima neta (N3, N14).
func (r *Reactor) OnPolicyIssued(ctx context.Context, repo Repository, ev policies.PolicyIssued) error {
	currency := kernel.Currency(ev.Currency)
	net, err := kernel.ParseMoney(ev.NetPremium, currency)
	if err != nil {
		return err
	}
	taxes, err := kernel.ParseMoney(ev.Taxes, currency)
	if err != nil {
		return err
	}
	from, err := kernel.ParseDate(ev.EffectiveFrom)
	if err != nil {
		return err
	}
	t := Terms{PolicyID: ev.PolicyID, InsurerID: ev.InsurerID, LineID: ev.LineOfBusinessID, ProductID: nullable(ev.ProductID), PolicyNumber: ev.PolicyNumber, Net: net, Taxes: taxes}
	if err := repo.UpsertTerms(ctx, t); err != nil {
		return err
	}
	if err := repo.InsertAgent(ctx, ev.PolicyID, nullable(ev.AgentID), from); err != nil {
		return err
	}
	a, found, err := r.agreement(ctx, t, from)
	if err != nil {
		return err
	}
	if !found || a.Basis != domain.BasisWritten {
		return nil
	}
	return r.accrue(ctx, repo, t, domain.KindWritten, ev.PolicyID, net, a, found, from)
}

// OnPolicyEndorsed actualiza la proyección y, con prima emitida, causa la variación.
func (r *Reactor) OnPolicyEndorsed(ctx context.Context, repo Repository, ev policies.PolicyEndorsed) error {
	t, ok, err := repo.GetTerms(ctx, ev.PolicyID)
	if err != nil || !ok {
		return err
	}
	currency := kernel.Currency(ev.Currency)
	netDelta, err := kernel.ParseMoney(ev.NetPremiumDelta, currency)
	if err != nil {
		return err
	}
	taxDelta, err := kernel.ParseMoney(ev.TaxesDelta, currency)
	if err != nil {
		return err
	}
	if t.Net, err = t.Net.Add(netDelta); err != nil {
		return err
	}
	if t.Taxes, err = t.Taxes.Add(taxDelta); err != nil {
		return err
	}
	if err := repo.UpsertTerms(ctx, t); err != nil {
		return err
	}
	on, err := kernel.ParseDate(ev.EffectiveDate)
	if err != nil {
		return err
	}
	a, found, err := r.agreement(ctx, t, on)
	if err != nil {
		return err
	}
	if !found || a.Basis != domain.BasisWritten {
		return nil
	}
	return r.accrue(ctx, repo, t, domain.KindEndorsement, ev.EndorsementID, netDelta, a, found, on)
}

// OnPaymentApplied causa sobre la parte neta del pago, en la fecha del pago (N3, N14, N16). Con base
// prima emitida ya se causó al expedir.
func (r *Reactor) OnPaymentApplied(ctx context.Context, repo Repository, ev billing.PaymentApplied) error {
	t, ok, err := repo.GetTerms(ctx, ev.PolicyID)
	if err != nil || !ok {
		return err
	}
	paid, err := kernel.ParseMoney(ev.Amount, kernel.Currency(ev.Currency))
	if err != nil {
		return err
	}
	on, err := kernel.ParseDate(ev.PaidOn)
	if err != nil {
		return err
	}
	a, found, err := r.agreement(ctx, t, on)
	if err != nil {
		return err
	}
	if found && a.Basis != domain.BasisPaid {
		return nil
	}
	// Un reverso entregado antes que su pago dejó una marca: el pago ya no causa.
	if _, reversed, err := repo.AccrualBySource(ctx, domain.KindPaymentReversal, ev.PaymentID); err != nil || reversed {
		return err
	}
	base, err := domain.NetShareOfPayment(paid, t.Net, t.Taxes)
	if err != nil {
		return err
	}
	return r.accrue(ctx, repo, t, domain.KindPayment, ev.PaymentID, base, a, found, on)
}

// OnPaymentReversed deshace lo causado por un pago. Si el pago aún no se procesó (reintentos), deja una
// marca en cero que impide causarlo después.
func (r *Reactor) OnPaymentReversed(ctx context.Context, repo Repository, ev billing.PaymentReversed) error {
	orig, ok, err := repo.AccrualBySource(ctx, domain.KindPayment, ev.PaymentID)
	if err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	now := r.Clock.Now().UTC()
	if ok {
		reversal, err := orig.Reversal(id)
		if err != nil {
			return err
		}
		return repo.InsertAccrual(ctx, reversal, now)
	}
	if _, known, err := repo.GetTerms(ctx, ev.PolicyID); err != nil || !known {
		return err
	}
	zero, err := kernel.NewPercentage(decimal.Zero)
	if err != nil {
		return err
	}
	marker, err := domain.NewReversalMarker(id, ev.PolicyID, ev.PaymentID, kernel.Currency(ev.Currency), zero, kernel.Today(r.Clock))
	if err != nil {
		return err
	}
	return repo.InsertAccrual(ctx, marker, now)
}

// OnPolicyCancelled causa el clawback sobre la prima no devengada informada, que es prima neta (T7).
func (r *Reactor) OnPolicyCancelled(ctx context.Context, repo Repository, ev policies.PolicyCancelled) error {
	t, ok, err := repo.GetTerms(ctx, ev.PolicyID)
	if err != nil || !ok {
		return err
	}
	unearned, err := kernel.ParseMoney(ev.UnearnedPremium, kernel.Currency(ev.Currency))
	if err != nil {
		return err
	}
	if unearned.IsZero() {
		return nil
	}
	on, err := kernel.ParseDate(ev.EffectiveDate)
	if err != nil {
		return err
	}
	a, found, err := r.agreement(ctx, t, on)
	if err != nil {
		return err
	}
	zero, err := kernel.Zero(unearned.Currency())
	if err != nil {
		return err
	}
	negative, err := zero.Sub(unearned)
	if err != nil {
		return err
	}
	return r.accrue(ctx, repo, t, domain.KindClawback, ev.PolicyID, negative, a, found, on)
}

// OnAgentChanged registra el cambio de asesor: lo causado desde esa fecha es del nuevo (N16).
func (r *Reactor) OnAgentChanged(ctx context.Context, repo Repository, ev policies.PolicyAgentChanged) error {
	_, ok, err := repo.GetTerms(ctx, ev.PolicyID)
	if err != nil || !ok {
		return err
	}
	at, err := time.Parse(time.RFC3339Nano, ev.EffectiveAt)
	if err != nil {
		return errors.Join(errors.New("commissions: fecha de traspaso inválida"), err)
	}
	return repo.InsertAgent(ctx, ev.PolicyID, nullable(ev.AgentID), kernel.DateOf(at, kernel.BusinessLocation()))
}
