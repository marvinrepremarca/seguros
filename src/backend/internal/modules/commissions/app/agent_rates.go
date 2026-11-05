package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ListRates consulta las tasas.
func (s *Service) ListRates(ctx context.Context) ([]AgentRate, error) {
	if err := authz.Require(ctx, authz.CommissionRateManage); err != nil {
		return nil, err
	}
	var out []AgentRate
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		out, err = uow.Commissions().ListRates(ctx)
		return err
	})
	return out, err
}

// CreateRate registra una tasa de asesor.
func (s *Service) CreateRate(ctx context.Context, r AgentRate) (AgentRate, error) {
	if err := authz.Require(ctx, authz.CommissionRateManage); err != nil {
		return AgentRate{}, err
	}
	if _, err := kernel.NewPercentage(r.Percentage); err != nil {
		return AgentRate{}, ErrInvalidRate
	}
	if r.To != nil && r.To.Before(r.From) {
		return AgentRate{}, ErrInvalidRate
	}
	id, err := newID()
	if err != nil {
		return AgentRate{}, err
	}
	r.ID = id
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Commissions().InsertRate(ctx, r, s.Clock.Now().UTC()); err != nil {
			return err
		}
		return uow.Audit(ctx, audit.Entry{
			Action: "commissions.rate.created", ResourceType: "commissions.rate", ResourceID: id.String(), Outcome: audit.OutcomeSuccess,
			Details: map[string]string{"percentage": r.Percentage.String()},
		})
	})
	return r, err
}
