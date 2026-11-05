package app

import (
	"context"
	"fmt"

	"github.com/marvinrepremarca/seguros/internal/modules/billing/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// ListSettings consulta los días de gracia.
func (s *Service) ListSettings(ctx context.Context) ([]ArrearsSetting, error) {
	if err := authz.Require(ctx, authz.BillingSettingsManage); err != nil {
		return nil, err
	}
	var out []ArrearsSetting
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		out, err = uow.Billing().ListSettings(ctx)
		return err
	})
	return out, err
}

// SetSetting cambia los días de gracia (DECISIONS N2).
func (s *Service) SetSetting(ctx context.Context, in ArrearsSetting) error {
	if err := authz.Require(ctx, authz.BillingSettingsManage); err != nil {
		return err
	}
	if err := domain.ValidateGraceDays(in.GraceDays); err != nil {
		return err
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Billing().SetSetting(ctx, in, s.Clock.Now().UTC()); err != nil {
			return err
		}
		return uow.Audit(ctx, audit.Entry{
			Action: "billing.settings.arrears_changed", ResourceType: "billing.settings", ResourceID: in.InsurerID.UUID.String(),
			Outcome: audit.OutcomeSuccess, Details: map[string]string{"grace_days": fmt.Sprint(in.GraceDays)},
		})
	})
}

// LeadDays consulta cuántos días antes del primer vencimiento se acepta un pago.
func (s *Service) LeadDays(ctx context.Context) (int32, error) {
	if err := authz.Require(ctx, authz.BillingSettingsManage); err != nil {
		return 0, err
	}
	var out int32
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		out, err = uow.Billing().LeadDays(ctx)
		return err
	})
	return out, err
}

// SetLeadDays cambia ese parámetro (DECISIONS N22).
func (s *Service) SetLeadDays(ctx context.Context, days int32) error {
	if err := authz.Require(ctx, authz.BillingSettingsManage); err != nil {
		return err
	}
	if days < 0 || days > 3650 {
		return domain.ErrInvalidLeadDays
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Billing().SetLeadDays(ctx, days, s.Clock.Now().UTC()); err != nil {
			return err
		}
		return uow.Audit(ctx, audit.Entry{
			Action: "billing.settings.lead_days_changed", ResourceType: "billing.settings", ResourceID: "lead_days", Outcome: audit.OutcomeSuccess,
			Details: map[string]string{"lead_days": fmt.Sprint(days)},
		})
	})
}
