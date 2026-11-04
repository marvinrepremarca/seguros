package app

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// CancelCommand registra la cancelación expedida por la aseguradora.
type CancelCommand struct {
	ID               domain.PolicyID
	Version          int32
	Cause            domain.CancellationCause
	EffectiveDate    kernel.Date
	UnearnedPremium  decimal.Decimal
	InsurerReference string
}

// Cancel registra la cancelación. La moneda de la prima no devengada es la de la póliza.
func (s *Service) Cancel(ctx context.Context, cmd CancelCommand) (View, error) {
	if err := authz.Require(ctx, authz.PolicyCancel); err != nil {
		return View{}, err
	}
	return s.change(ctx, cmd.ID, cmd.Version, "policies.policy.cancelled", func(p *domain.Policy) (map[string]string, error) {
		unearned, err := kernel.NewMoney(cmd.UnearnedPremium, p.Snapshot().Terms.NetPremium.Currency())
		if err != nil {
			return nil, err
		}
		return map[string]string{"cause": string(cmd.Cause)}, p.Cancel(domain.Cancellation{
			Cause: cmd.Cause, EffectiveDate: cmd.EffectiveDate, UnearnedPremium: unearned, InsurerReference: cmd.InsurerReference,
		}, s.Clock.Now())
	})
}
