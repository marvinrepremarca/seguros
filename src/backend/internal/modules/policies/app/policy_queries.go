package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Get devuelve una póliza. Un asesor con read-own recibe 404 para las de otro: no revela que existen.
func (s *Service) Get(ctx context.Context, id domain.PolicyID) (View, error) {
	scope, err := readScope(ctx)
	if err != nil {
		return View{}, err
	}
	var snap domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := uow.Policies().Get(ctx, id)
		if err != nil {
			return err
		}
		snap = p.Snapshot()
		return nil
	})
	if err != nil {
		return View{}, err
	}
	if scope.Valid && snap.Terms.AgentID != scope {
		return View{}, domain.ErrNotFound
	}
	return s.view(ctx, snap)
}

// ListQuery filtra y pagina.
type ListQuery struct {
	Status domain.Status
	Cursor string
	Limit  int32
}

// Page es una página de pólizas.
type Page struct {
	Items      []Summary
	NextCursor string
}

// List lista las pólizas que el usuario puede ver, con el nombre del tomador.
func (s *Service) List(ctx context.Context, q ListQuery) (Page, error) {
	scope, err := readScope(ctx)
	if err != nil {
		return Page{}, err
	}
	before, err := kernel.DecodeCursor(q.Cursor)
	if err != nil {
		return Page{}, err
	}
	size := kernel.PageSize(q.Limit)
	var rows []Summary
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		rows, err = uow.Policies().List(ctx, ListFilter{Before: before, Agent: scope, Status: q.Status, Limit: size + 1})
		return err
	})
	if err != nil {
		return Page{}, err
	}
	var page Page
	if int32(len(rows)) > size { //nolint:gosec // acotado por MaxPageSize + 1
		rows = rows[:size]
		page.NextCursor = kernel.EncodeCursor(uuid.UUID(rows[size-1].ID))
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.HolderPartyID)
	}
	names, err := s.Parties.Summaries(ctx, ids)
	if err != nil {
		return Page{}, fmt.Errorf("policies: consultando tomadores: %w", err)
	}
	for i := range rows {
		rows[i].HolderName = names[rows[i].HolderPartyID].Name
	}
	page.Items = rows
	return page, nil
}
