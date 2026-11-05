package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/quoting/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// Decline cierra la solicitud. El motivo es texto libre: no va a la bitácora.
func (s *Service) Decline(ctx context.Context, id uuid.UUID, reason string) (View, error) {
	if err := authz.Require(ctx, authz.QuoteManageOwn); err != nil {
		return View{}, err
	}
	user, err := currentUser(ctx)
	if err != nil {
		return View{}, err
	}
	var snap domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		r, err := loadOwn(ctx, uow, id, user)
		if err != nil {
			return err
		}
		if err := r.Decline(reason); err != nil {
			return err
		}
		if err := uow.Requests().Save(ctx, r, s.Clock.Now().UTC()); err != nil {
			return err
		}
		snap = r.Snapshot()
		return uow.Audit(ctx, entry("quoting.request.declined", id))
	})
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, snap)
}
