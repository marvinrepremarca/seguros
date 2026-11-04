package app

import (
	"context"
	"fmt"

	identity "github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// ListAgents lista los usuarios que se pueden asignar como asesor.
func (s *Service) ListAgents(ctx context.Context) ([]identity.User, error) {
	if err := authz.Require(ctx, authz.PolicyManage); err != nil {
		return nil, err
	}
	users, err := s.Users.Users(ctx)
	if err != nil {
		return nil, fmt.Errorf("policies: consultando asesores: %w", err)
	}
	return users, nil
}
