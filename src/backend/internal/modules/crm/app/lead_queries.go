package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/crm/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Get devuelve un prospecto; fuera del alcance, como inexistente.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.Snapshot, error) {
	scope, err := readScope(ctx)
	if err != nil {
		return domain.Snapshot{}, err
	}
	var snap domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		l, err := uow.Leads().Get(ctx, id)
		if err != nil {
			return err
		}
		snap = l.Snapshot()
		return nil
	})
	if err != nil {
		return domain.Snapshot{}, err
	}
	if scope.Valid && snap.AgentID != scope {
		return domain.Snapshot{}, domain.ErrNotFound
	}
	return snap, nil
}

// List lista los prospectos visibles para el usuario.
func (s *Service) List(ctx context.Context, status, cursor string, limit int32) ([]domain.Snapshot, string, error) {
	scope, err := readScope(ctx)
	if err != nil {
		return nil, "", err
	}
	before, err := kernel.DecodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	size := kernel.PageSize(limit)
	var rows []domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		rows, err = uow.Leads().List(ctx, status, scope, before, size+1)
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
	return rows, next, nil
}
