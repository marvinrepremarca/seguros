package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// Accept registra que el cliente aceptó.
func (s *Service) Accept(ctx context.Context, id domain.PolicyID, version int32) (View, error) {
	if err := authz.Require(ctx, authz.PolicyManage); err != nil {
		return View{}, err
	}
	return s.change(ctx, id, version, "policies.policy.accepted", func(p *domain.Policy) (map[string]string, error) {
		return nil, p.Accept(s.Clock.Now())
	})
}
