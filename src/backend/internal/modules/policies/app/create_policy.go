package app

import (
	"context"
	"fmt"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Create registra una póliza en cotización (expedición manual).
func (s *Service) Create(ctx context.Context, in TermsInput) (View, error) {
	if err := authz.Require(ctx, authz.PolicyManage); err != nil {
		return View{}, err
	}
	terms, err := s.buildTerms(ctx, in)
	if err != nil {
		return View{}, err
	}
	id, err := kernel.NewID[domain.PolicyID]()
	if err != nil {
		return View{}, fmt.Errorf("policies: %w", err)
	}
	policy, err := domain.NewPolicy(id, terms, s.Clock.Now())
	if err != nil {
		return View{}, err
	}
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Policies().Insert(ctx, policy); err != nil {
			return err
		}
		return record(ctx, uow, policy, "policies.policy.created", "", nil)
	})
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, policy.Snapshot())
}
