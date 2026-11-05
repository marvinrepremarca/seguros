package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ListAgentCommissions consulta la comisión interna.
func (s *Service) ListAgentCommissions(ctx context.Context, agent uuid.NullUUID, cursor string, limit int32) ([]AgentCommission, string, error) {
	if err := authz.Require(ctx, authz.CommissionRead); err != nil {
		return nil, "", err
	}
	after, err := kernel.DecodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	size := kernel.PageSize(limit)
	var rows []AgentCommission
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		rows, err = uow.Commissions().ListAgentCommissions(ctx, agent, after, size+1)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	items, next := page(rows, size, func(c AgentCommission) uuid.UUID { return c.ID })
	return items, next, nil
}
