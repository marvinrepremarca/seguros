package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/claims/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Get devuelve un siniestro. El alcance del asesor sale de la póliza en este momento: tras un traspaso,
// lo ve el asesor nuevo y no el anterior.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (View, error) {
	scope, err := ownScope(ctx)
	if err != nil {
		return View{}, err
	}
	var snap domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		c, err := uow.Claims().Get(ctx, id)
		if err != nil {
			return err
		}
		snap = c.Snapshot()
		return nil
	})
	if err != nil {
		return View{}, err
	}
	if scope.Valid {
		facts, found, err := s.Policies.Facts(ctx, snap.PolicyID)
		if err != nil {
			return View{}, fmt.Errorf("claims: consultando póliza: %w", err)
		}
		if !found || facts.AgentID != scope {
			return View{}, domain.ErrNotFound
		}
	}
	return s.view(ctx, snap)
}

// List lista los siniestros visibles.
func (s *Service) List(ctx context.Context, status, cursor string, limit int32) ([]View, string, error) {
	scope, err := ownScope(ctx)
	if err != nil {
		return nil, "", err
	}
	before, err := kernel.DecodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	size := kernel.PageSize(limit)
	f := ListFilter{Status: status, Before: before, Limit: size + 1, Scoped: scope.Valid}
	if scope.Valid {
		f.Policies, err = s.Policies.IDsOfAgent(ctx, scope.UUID, maxAgentPolicies)
		if err != nil {
			return nil, "", fmt.Errorf("claims: consultando pólizas: %w", err)
		}
	}
	var rows []domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		rows, err = uow.Claims().List(ctx, f)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if int32(len(rows)) > size { //nolint:gosec // acotado por el límite
		rows = rows[:size]
		next = kernel.EncodeCursor(rows[size-1].ID)
	}
	out, err := s.views(ctx, rows)
	return out, next, err
}
