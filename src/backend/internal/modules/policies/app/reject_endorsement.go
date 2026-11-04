package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// RejectEndorsement registra que la aseguradora no emitió el endoso.
func (s *Service) RejectEndorsement(ctx context.Context, id domain.PolicyID, version int32, endorsement uuid.UUID, reason string) (View, error) {
	if err := authz.Require(ctx, authz.EndorsementManage); err != nil {
		return View{}, err
	}
	return s.change(ctx, id, version, "policies.endorsement.rejected", func(p *domain.Policy) (map[string]string, error) {
		return map[string]string{"endorsementId": endorsement.String()}, p.RejectEndorsement(endorsement, reason, s.Clock.Now())
	})
}
