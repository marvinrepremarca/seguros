package app

import (
	"context"
	"fmt"

	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// GetRenewalSettings consulta qué conserva una renovación.
func (s *Service) GetRenewalSettings(ctx context.Context) (RenewalSettings, error) {
	if err := authz.Require(ctx, authz.PolicySettingsManage); err != nil {
		return RenewalSettings{}, err
	}
	var out RenewalSettings
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		out, err = uow.Policies().RenewalSettings(ctx)
		return err
	})
	return out, err
}

// SetRenewalSettings cambia qué conserva una renovación (DECISIONS N25).
func (s *Service) SetRenewalSettings(ctx context.Context, v RenewalSettings) error {
	if err := authz.Require(ctx, authz.PolicySettingsManage); err != nil {
		return err
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Policies().SetRenewalSettings(ctx, v, s.Clock.Now().UTC()); err != nil {
			return err
		}
		return uow.Audit(ctx, audit.Entry{
			Action: "policies.settings.renewal_changed", ResourceType: "policies.settings", ResourceID: "renewal", Outcome: audit.OutcomeSuccess,
			Details: map[string]string{"keep_holder": fmt.Sprint(v.KeepHolder), "keep_line": fmt.Sprint(v.KeepLine)},
		})
	})
}
