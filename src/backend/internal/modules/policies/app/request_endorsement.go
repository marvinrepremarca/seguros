package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// RequestEndorsement registra una solicitud de endoso.
func (s *Service) RequestEndorsement(ctx context.Context, id domain.PolicyID, version int32, kind domain.EndorsementType, description string, effective kernel.Date) (View, error) {
	if err := authz.Require(ctx, authz.EndorsementManage); err != nil {
		return View{}, err
	}
	endorsement, err := uuid.NewV7()
	if err != nil {
		return View{}, fmt.Errorf("policies: %w", err)
	}
	return s.change(ctx, id, version, "policies.endorsement.requested", func(p *domain.Policy) (map[string]string, error) {
		return map[string]string{"endorsementId": endorsement.String(), "type": string(kind)}, p.RequestEndorsement(endorsement, kind, description, effective, s.Clock.Now())
	})
}
