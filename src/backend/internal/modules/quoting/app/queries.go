package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/quoting/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Get devuelve una solicitud con sus ofertas: el comparativo.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (View, error) {
	scope, err := readScope(ctx)
	if err != nil {
		return View{}, err
	}
	var snap domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		r, err := uow.Requests().Get(ctx, id)
		if err != nil {
			return err
		}
		snap = r.Snapshot()
		return nil
	})
	if err != nil {
		return View{}, err
	}
	if scope.Valid && snap.AgentID != scope.UUID {
		return View{}, domain.ErrNotFound
	}
	return s.view(ctx, snap)
}

// List lista las solicitudes visibles, sin ofertas.
func (s *Service) List(ctx context.Context, status, cursor string, limit int32) ([]View, string, error) {
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
		rows, err = uow.Requests().List(ctx, status, scope, before, size+1)
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
