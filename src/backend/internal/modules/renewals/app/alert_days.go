package app

import (
	"context"
	"fmt"

	"github.com/marvinrepremarca/seguros/internal/modules/renewals/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// AlertDays consulta los días de alerta.
func (s *Service) AlertDays(ctx context.Context) ([]int32, error) {
	if err := authz.Require(ctx, authz.RenewalSettingsManage); err != nil {
		return nil, err
	}
	var out []int32
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		out, err = uow.Renewals().AlertDays(ctx)
		return err
	})
	return out, err
}

// SetAlertDays cambia los días de alerta (DECISIONS T2).
func (s *Service) SetAlertDays(ctx context.Context, days []int32) error {
	if err := authz.Require(ctx, authz.RenewalSettingsManage); err != nil {
		return err
	}
	if err := domain.ValidateAlertDays(days); err != nil {
		return err
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Renewals().SetAlertDays(ctx, days, s.Clock.Now().UTC()); err != nil {
			return err
		}
		return uow.Audit(ctx, audit.Entry{
			Action: "renewals.settings.alert_days_changed", ResourceType: "renewals.settings", ResourceID: "alert_days", Outcome: audit.OutcomeSuccess,
			Details: map[string]string{"days": fmt.Sprint(days)},
		})
	})
}
