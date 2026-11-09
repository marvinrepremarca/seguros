package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/modules/quoting/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// OfferValidDays lee el parámetro T3.
func (s *Service) OfferValidDays(ctx context.Context) (int32, error) {
	if err := authz.Require(ctx, authz.QuoteSettingsManage); err != nil {
		return 0, err
	}
	var days int32
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		days, err = uow.Requests().OfferValidDays(ctx)
		return err
	})
	return days, err
}

// SetOfferValidDays cambia el parámetro T3.
func (s *Service) SetOfferValidDays(ctx context.Context, days int32) error {
	if err := authz.Require(ctx, authz.QuoteSettingsManage); err != nil {
		return err
	}
	if err := domain.ValidateOfferValidDays(days); err != nil {
		return err
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Requests().SetOfferValidDays(ctx, days, s.Clock.Now().UTC()); err != nil {
			return err
		}
		return uow.Audit(ctx, audit.Entry{Action: "quoting.settings.changed", ResourceType: "quoting.settings", ResourceID: "offer_valid_days", Outcome: audit.OutcomeSuccess})
	})
}
