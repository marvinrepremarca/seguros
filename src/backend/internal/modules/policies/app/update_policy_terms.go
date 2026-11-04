package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// UpdateTerms ajusta los datos antes de expedir.
func (s *Service) UpdateTerms(ctx context.Context, id domain.PolicyID, version int32, in TermsInput) (View, error) {
	if err := authz.Require(ctx, authz.PolicyManage); err != nil {
		return View{}, err
	}
	terms, err := s.buildTerms(ctx, in)
	if err != nil {
		return View{}, err
	}
	return s.change(ctx, id, version, "policies.policy.terms_updated", func(p *domain.Policy) (map[string]string, error) {
		return nil, p.UpdateTerms(terms, s.Clock.Now())
	})
}
