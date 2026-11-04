package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// Reject registra el rechazo de la aseguradora.
func (s *Service) Reject(ctx context.Context, id domain.PolicyID, version int32, reason string) (View, error) {
	if err := authz.Require(ctx, authz.PolicyManage); err != nil {
		return View{}, err
	}
	return s.change(ctx, id, version, "policies.policy.rejected", func(p *domain.Policy) (map[string]string, error) {
		return nil, p.Reject(reason, s.Clock.Now())
	})
}
