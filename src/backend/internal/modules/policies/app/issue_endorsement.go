package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// CoverageChangeInput cambia la suma asegurada de una cobertura.
type CoverageChangeInput struct {
	CoverageID uuid.UUID
	SumInsured decimal.Decimal
}

// IssueEndorsementCommand registra el endoso emitido por la aseguradora.
type IssueEndorsementCommand struct {
	PolicyID        domain.PolicyID
	Version         int32
	EndorsementID   uuid.UUID
	Number          string
	NetPremiumDelta decimal.Decimal
	TaxesDelta      decimal.Decimal
	CoverageChanges []CoverageChangeInput
	AddParties      []domain.PolicyParty
	RemoveParties   []domain.PolicyParty
	ItemChanges     []domain.ItemChange
}

// IssueEndorsement aplica el endoso. Las partes nuevas se validan contra Parties antes de la
// transacción; los datos de riesgo, contra el esquema vigente del bien.
func (s *Service) IssueEndorsement(ctx context.Context, cmd IssueEndorsementCommand) (View, error) {
	if err := authz.Require(ctx, authz.EndorsementManage); err != nil {
		return View{}, err
	}
	if len(cmd.AddParties) > 0 {
		if err := s.checkParties(ctx, cmd.AddParties); err != nil {
			return View{}, err
		}
	}
	return s.change(ctx, cmd.PolicyID, cmd.Version, "policies.endorsement.issued", func(p *domain.Policy) (map[string]string, error) {
		snap := p.Snapshot()
		currency := snap.Terms.NetPremium.Currency()
		net, err := kernel.NewMoney(cmd.NetPremiumDelta, currency)
		if err != nil {
			return nil, err
		}
		taxes, err := kernel.NewMoney(cmd.TaxesDelta, currency)
		if err != nil {
			return nil, err
		}
		effects := domain.EndorsementEffects{AddParties: cmd.AddParties, RemoveParties: cmd.RemoveParties, ItemChanges: cmd.ItemChanges}
		for _, c := range cmd.CoverageChanges {
			sum, err := kernel.NewMoney(c.SumInsured, currency)
			if err != nil {
				return nil, err
			}
			effects.CoverageChanges = append(effects.CoverageChanges, domain.CoverageChange{CoverageID: c.CoverageID, SumInsured: sum})
		}
		for _, c := range cmd.ItemChanges {
			for _, item := range snap.Terms.Items {
				if item.ID != c.ItemID {
					continue
				}
				if err := s.Schemas.Validate(item.RiskSchemaKey, c.RiskData); err != nil {
					return nil, err
				}
			}
		}
		return map[string]string{
			"endorsementId": cmd.EndorsementID.String(), "netPremiumDelta": net.Amount().String(), "taxesDelta": taxes.Amount().String(),
		}, p.IssueEndorsement(cmd.EndorsementID, cmd.Number, net, taxes, effects, s.Clock.Now())
	})
}
