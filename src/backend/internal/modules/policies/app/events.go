package app

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
)

func publish(ctx context.Context, uow UnitOfWork, events []domain.Event) error {
	for _, e := range events {
		if changed, ok := e.(domain.PolicyAgentChanged); ok {
			if err := uow.Policies().AppendAgentHistory(ctx, changed); err != nil {
				return err
			}
		}
		topic, payload, err := toContract(e)
		if err != nil {
			return err
		}
		if err := uow.Publish(ctx, topic, payload); err != nil {
			return err
		}
	}
	return nil
}

func nullable(id uuid.NullUUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	return &id.UUID
}

func toContract(e domain.Event) (string, any, error) {
	switch ev := e.(type) {
	case domain.PolicyPartiesChanged:
		return contracts.TopicPolicyPartiesChanged, contracts.PolicyPartiesChanged{
			PolicyID: uuid.UUID(ev.PolicyID), PolicyVersion: ev.Version, AgentID: nullable(ev.AgentID), PartyIDs: ev.PartyIDs, GrantsAccess: ev.GrantsAccess,
		}, nil
	case domain.PolicyAgentChanged:
		return contracts.TopicPolicyAgentChanged, contracts.PolicyAgentChanged{
			PolicyID: uuid.UUID(ev.PolicyID), PreviousAgentID: nullable(ev.Previous), AgentID: nullable(ev.New), EffectiveAt: ev.At.Format(time.RFC3339Nano),
		}, nil
	case domain.PolicyIssued:
		t := ev.Policy.Terms
		holder := uuid.Nil
		for _, p := range t.Parties {
			if p.Role == domain.RoleHolder {
				holder = p.PartyID
			}
		}
		return contracts.TopicPolicyIssued, contracts.PolicyIssued{
			PolicyID: uuid.UUID(ev.Policy.ID), PolicyNumber: ev.Policy.Number, InsurerID: t.InsurerID, LineOfBusinessID: t.LineOfBusinessID,
			ProductID: nullable(t.ProductID), HolderPartyID: holder, AgentID: nullable(t.AgentID), Currency: string(t.NetPremium.Currency()),
			NetPremium: t.NetPremium.Amount().String(), Taxes: t.Taxes.Amount().String(),
			EffectiveFrom: t.Period.From().String(), EffectiveTo: t.Period.To().String(), PaymentForm: string(t.PaymentForm),
			RenewedFromPolicyID: nullable(ev.Policy.RenewedFrom),
		}, nil
	case domain.PolicyEndorsed:
		return contracts.TopicPolicyEndorsed, contracts.PolicyEndorsed{
			PolicyID: uuid.UUID(ev.PolicyID), EndorsementID: ev.EndorsementID, Currency: string(ev.NetPremiumDelta.Currency()),
			NetPremiumDelta: ev.NetPremiumDelta.Amount().String(), TaxesDelta: ev.TaxesDelta.Amount().String(), EffectiveDate: ev.EffectiveDate.String(),
		}, nil
	case domain.PolicyCancelled:
		c := ev.Cancellation
		return contracts.TopicPolicyCancelled, contracts.PolicyCancelled{
			PolicyID: uuid.UUID(ev.PolicyID), Cause: string(c.Cause), EffectiveDate: c.EffectiveDate.String(),
			Currency: string(c.UnearnedPremium.Currency()), UnearnedPremium: c.UnearnedPremium.Amount().String(),
		}, nil
	case domain.PolicyExpired:
		return contracts.TopicPolicyExpired, contracts.PolicyExpired{PolicyID: uuid.UUID(ev.PolicyID)}, nil
	case domain.PolicyRenewed:
		return contracts.TopicPolicyRenewed, contracts.PolicyRenewed{PolicyID: uuid.UUID(ev.PolicyID), RenewedBy: uuid.UUID(ev.RenewedBy)}, nil
	case domain.PolicyFellIntoArrears:
		return contracts.TopicPolicyFellIntoArrears, contracts.PolicyFellIntoArrears{PolicyID: uuid.UUID(ev.PolicyID)}, nil
	case domain.PolicyReinstated:
		return contracts.TopicPolicyReinstated, contracts.PolicyReinstated{PolicyID: uuid.UUID(ev.PolicyID)}, nil
	default:
		return "", nil, fmt.Errorf("policies: evento sin contrato: %T", e)
	}
}
