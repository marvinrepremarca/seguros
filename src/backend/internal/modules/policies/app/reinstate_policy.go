package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// Reinstate registra que la aseguradora confirmó el pago: EnMora → Vigente (DECISIONS N24).
func (s *Service) Reinstate(ctx context.Context, id domain.PolicyID, version int32) (View, error) {
	if err := authz.Require(ctx, authz.PolicyManage); err != nil {
		return View{}, err
	}
	return s.change(ctx, id, version, "policies.policy.reinstated", func(p *domain.Policy) (map[string]string, error) {
		return nil, p.Reinstate(s.Clock.Now())
	})
}
