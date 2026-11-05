package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	quoting "github.com/marvinrepremarca/seguros/internal/modules/quoting/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// CreateFromQuote crea la póliza EnTramite de una oferta aceptada (DECISIONS N28). Quoting ya validó
// catálogo, esquema de riesgo, partes y asesor al aceptar. El inbox evita duplicarla si se reentrega.
func CreateFromQuote(ctx context.Context, uow UnitOfWork, clock kernel.Clock, ev quoting.QuoteAccepted) error {
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
	to, err := kernel.ParseDate(ev.EffectiveTo)
	if err != nil {
		return err
	}
	period, err := kernel.NewDateRange(from, to)
	if err != nil {
		return err
	}
	terms := domain.Terms{
		InsurerID: ev.InsurerID, LineOfBusinessID: ev.LineOfBusinessID, NetPremium: net, Taxes: taxes, Period: period, PaymentForm: domain.PaymentForm(ev.PaymentForm),
		Parties: []domain.PolicyParty{{PartyID: ev.HolderPartyID, Role: domain.RoleHolder}},
	}
	if ev.ProductID != nil {
		terms.ProductID = uuid.NullUUID{UUID: *ev.ProductID, Valid: true}
	}
	if ev.AgentID != nil {
		terms.AgentID = uuid.NullUUID{UUID: *ev.AgentID, Valid: true}
	}
	for _, p := range ev.InsuredPartyIDs {
		terms.Parties = append(terms.Parties, domain.PolicyParty{PartyID: p, Role: domain.RoleInsured})
	}
	for _, c := range ev.Coverages {
		sum, err := kernel.ParseMoney(c.SumInsured, currency)
		if err != nil {
			return err
		}
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("policies: %w", err)
		}
		terms.Coverages = append(terms.Coverages, domain.Coverage{ID: id, Name: c.Name, SumInsured: sum, Deductible: c.Deductible})
	}
	item, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("policies: %w", err)
	}
	terms.Items = []domain.InsuredItem{{ID: item, Description: ev.RiskDescription, RiskSchemaKey: ev.RiskSchemaKey, RiskSchemaVersion: ev.RiskSchemaVersion, RiskData: ev.RiskData}}
	now := clock.Now()
	p, err := domain.NewPolicy(domain.PolicyID(ev.PolicyID), terms, now)
	if err != nil {
		return err
	}
	if err := p.Accept(now); err != nil {
		return err
	}
	if err := uow.Policies().Insert(ctx, p); err != nil {
		return err
	}
	return record(ctx, uow, p, "policies.policy.created_from_quote", "", map[string]string{"quoteRequest": ev.RequestID.String()})
}
