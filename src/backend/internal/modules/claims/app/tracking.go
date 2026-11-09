package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/claims/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

func (s *Service) track(ctx context.Context, claim uuid.UUID, action string, fn func(context.Context, UnitOfWork) error) error {
	if err := authz.Require(ctx, authz.ClaimManage); err != nil {
		return err
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if _, err := uow.Claims().Get(ctx, claim); err != nil {
			return err
		}
		if err := fn(ctx, uow); err != nil {
			return err
		}
		return uow.Audit(ctx, entry(action, claim))
	})
}

// AddChecklistItem agrega un soporte esperado.
func (s *Service) AddChecklistItem(ctx context.Context, claim uuid.UUID, label string) error {
	if err := domain.ValidateLabel(label); err != nil {
		return err
	}
	return s.track(ctx, claim, "claims.checklist.added", func(ctx context.Context, uow UnitOfWork) error {
		return uow.Claims().AddChecklistItem(ctx, claim, label, s.Clock.Now().UTC())
	})
}

// SetChecklistItem marca si el soporte se recibió.
func (s *Service) SetChecklistItem(ctx context.Context, claim, item uuid.UUID, received bool) error {
	return s.track(ctx, claim, "claims.checklist.changed", func(ctx context.Context, uow UnitOfWork) error {
		return uow.Claims().SetChecklistItem(ctx, claim, item, received, s.Clock.Now().UTC())
	})
}

// AddNote registra una gestión con la aseguradora.
func (s *Service) AddNote(ctx context.Context, claim uuid.UUID, body string) error {
	if err := domain.ValidateNote(body); err != nil {
		return err
	}
	return s.track(ctx, claim, "claims.note.added", func(ctx context.Context, uow UnitOfWork) error {
		return uow.Claims().AddNote(ctx, claim, body, s.Clock.Now().UTC())
	})
}

// Tracking devuelve lista de control y gestiones.
func (s *Service) Tracking(ctx context.Context, claim uuid.UUID) ([]ChecklistItem, []Note, error) {
	if err := authz.Require(ctx, authz.ClaimRead); err != nil {
		return nil, nil, err
	}
	var items []ChecklistItem
	var notes []Note
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if _, err := uow.Claims().Get(ctx, claim); err != nil {
			return err
		}
		var err error
		if items, err = uow.Claims().Checklist(ctx, claim); err != nil {
			return err
		}
		notes, err = uow.Claims().Notes(ctx, claim)
		return err
	})
	return items, notes, err
}
