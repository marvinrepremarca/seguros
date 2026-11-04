package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// ReassignAgent cambia el asesor de una póliza a otro activo (DECISIONS N11).
func (s *Service) ReassignAgent(ctx context.Context, id domain.PolicyID, version int32, agent uuid.UUID, reason string) (View, error) {
	if err := authz.Require(ctx, authz.PolicyAssign); err != nil {
		return View{}, err
	}
	reason, err := domain.ValidTransferReason(reason)
	if err != nil {
		return View{}, err
	}
	if err := s.checkActiveAgent(ctx, agent); err != nil {
		return View{}, err
	}
	return s.change(ctx, id, version, "policies.policy.agent_changed", func(p *domain.Policy) (map[string]string, error) {
		details := map[string]string{"new": agent.String(), "reason": reason}
		if previous := p.Snapshot().Terms.AgentID; previous.Valid {
			details["previous"] = previous.UUID.String()
		}
		return details, p.ReassignAgent(agent, reason, s.Clock.Now())
	})
}
