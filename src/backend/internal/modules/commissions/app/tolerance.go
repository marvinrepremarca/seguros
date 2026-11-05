package app

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/commissions/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Tolerances consulta las tolerancias fijadas; COP siempre aparece, con su valor por defecto si no se fijó.
func (s *Service) Tolerances(ctx context.Context) ([]kernel.Money, error) {
	if err := authz.Require(ctx, authz.CommissionRateManage); err != nil {
		return nil, err
	}
	var out []kernel.Money
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		rows, err := uow.Commissions().ListTolerances(ctx)
		if err != nil {
			return err
		}
		out = rows
		for _, m := range rows {
			if m.Currency() == "COP" {
				return nil
			}
		}
		cop, err := domain.DefaultTolerance("COP")
		if err != nil {
			return err
		}
		out = append([]kernel.Money{cop}, rows...)
		return nil
	})
	return out, err
}

// SetTolerance cambia la tolerancia de una moneda (DECISIONS T4, N26).
func (s *Service) SetTolerance(ctx context.Context, currency string, v decimal.Decimal) error {
	if err := authz.Require(ctx, authz.CommissionRateManage); err != nil {
		return err
	}
	m, err := kernel.NewMoney(v, kernel.Currency(currency))
	if err != nil {
		return domain.ErrInvalidTolerance
	}
	if err := domain.ValidateTolerance(m); err != nil {
		return err
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Commissions().SetTolerance(ctx, m, s.Clock.Now().UTC()); err != nil {
			return err
		}
		return uow.Audit(ctx, audit.Entry{
			Action: "commissions.settings.tolerance_changed", ResourceType: "commissions.settings", ResourceID: currency, Outcome: audit.OutcomeSuccess,
			Details: map[string]string{"tolerance": v.String()},
		})
	})
}
