package app

import (
	"context"
	"fmt"

	identity "github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// ListAgents lista los usuarios de la agencia; solo a los activos se les asigna cartera.
func (s *Service) ListAgents(ctx context.Context) ([]identity.User, error) {
	// Quien expide elige asesor y quien traspasa cartera también: son permisos alternativos.
	if err := authz.Require(ctx, authz.PolicyManage); err != nil {
		if err := authz.Require(ctx, authz.PolicyAssign); err != nil {
			return nil, err
		}
	}
	users, err := s.Users.Agents(ctx)
	if err != nil {
		return nil, fmt.Errorf("policies: consultando asesores: %w", err)
	}
	return users, nil
}
