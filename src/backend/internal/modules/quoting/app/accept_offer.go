package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"

	catalog "github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/quoting/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/quoting/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// maxEventBytes deja margen frente al límite del outbox (16 KiB).
const maxEventBytes = 15 << 10

// AcceptInput confirma lo que falta para la póliza (DECISIONS N28).
type AcceptInput struct {
	OfferID     uuid.UUID
	PaymentForm string
	// Insured vacío significa que el tomador es el asegurado.
	Insured []uuid.UUID
}

// Accept acepta la oferta y publica QuoteAccepted en la misma transacción.
func (s *Service) Accept(ctx context.Context, id uuid.UUID, in AcceptInput) (View, error) {
	if err := authz.Require(ctx, authz.QuoteManageOwn); err != nil {
		return View{}, err
	}
	user, err := currentUser(ctx)
	if err != nil {
		return View{}, err
	}
	if err := domain.ValidatePaymentForm(in.PaymentForm); err != nil {
		return View{}, err
	}
	var current domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		r, err := loadOwn(ctx, uow, id, user)
		if err != nil {
			return err
		}
		current = r.Snapshot()
		return nil
	})
	if err != nil {
		return View{}, err
	}
	insured := in.Insured
	if len(insured) == 0 {
		insured = []uuid.UUID{current.HolderPartyID}
	}
	// Un repetido haría fallar el consumidor de Policies, con la oferta ya aceptada.
	slices.SortFunc(insured, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	insured = slices.Compact(insured)
	// Lo que el consumidor de Policies da por válido se comprueba aquí, cuando aún se puede responder.
	if err := s.checkVisible(ctx, append([]uuid.UUID{current.HolderPartyID}, insured...), user); err != nil {
		return View{}, err
	}
	agent, ok, err := s.Users.Agent(ctx, user)
	if err != nil {
		return View{}, fmt.Errorf("quoting: consultando asesor: %w", err)
	}
	if !ok || !agent.Active {
		return View{}, domain.ErrInactiveAgent
	}
	for _, o := range current.Offers {
		if o.ID == in.OfferID {
			if _, err := s.Catalog.ResolvePolicyRef(ctx, catalog.PolicyRef{InsurerID: o.InsurerID, LineOfBusinessID: current.LineOfBusinessID, ProductID: o.ProductID}); err != nil {
				return View{}, err
			}
		}
	}
	policyID, err := uuid.NewV7()
	if err != nil {
		return View{}, fmt.Errorf("quoting: %w", err)
	}
	var snap domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		r, err := loadOwn(ctx, uow, id, user)
		if err != nil {
			return err
		}
		offer, err := r.Accept(in.OfferID, policyID, kernel.Today(s.Clock))
		if err != nil {
			return err
		}
		if err := uow.Requests().Save(ctx, r, s.Clock.Now().UTC()); err != nil {
			return err
		}
		snap = r.Snapshot()
		ev := accepted(snap, offer, in.PaymentForm, insured)
		raw, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("quoting: %w", err)
		}
		if len(raw) > maxEventBytes {
			return domain.ErrTooLarge
		}
		if err := uow.Publish(ctx, contracts.TopicQuoteAccepted, ev); err != nil {
			return err
		}
		e := entry("quoting.offer.accepted", id)
		e.Details = map[string]string{"offer": offer.ID.String(), "policy": policyID.String()}
		return uow.Audit(ctx, e)
	})
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, snap)
}

func accepted(s domain.Snapshot, o domain.Offer, form string, insured []uuid.UUID) contracts.QuoteAccepted {
	agent := s.AgentID
	ev := contracts.QuoteAccepted{
		RequestID: s.ID, OfferID: o.ID, PolicyID: s.PolicyID.UUID, InsurerID: o.InsurerID, LineOfBusinessID: s.LineOfBusinessID, AgentID: &agent, HolderPartyID: s.HolderPartyID,
		InsuredPartyIDs: insured, Currency: string(o.NetPremium.Currency()), NetPremium: o.NetPremium.Amount().String(), Taxes: o.Taxes.Amount().String(),
		EffectiveFrom: o.EffectiveFrom.String(), EffectiveTo: o.EffectiveTo.String(), PaymentForm: form,
		RiskDescription: s.RiskDescription, RiskSchemaKey: o.RiskSchemaKey, RiskSchemaVersion: o.RiskSchemaVersion, RiskData: s.RiskData,
	}
	if s.LeadID.Valid {
		lead := s.LeadID.UUID
		ev.LeadID = &lead
	}
	if o.ProductID.Valid {
		product := o.ProductID.UUID
		ev.ProductID = &product
	}
	for _, c := range o.Coverages {
		ev.Coverages = append(ev.Coverages, contracts.QuoteCoverage{Name: c.Name, SumInsured: c.SumInsured.Amount().String(), Deductible: c.Deductible})
	}
	return ev
}
