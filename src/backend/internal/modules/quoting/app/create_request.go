package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/quoting/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// CreateInput son los datos de una solicitud.
type CreateInput struct {
	LeadID           uuid.NullUUID
	HolderPartyID    uuid.UUID
	LineOfBusinessID uuid.UUID
	RiskDescription  string
	RiskData         []byte
}

// Create registra una solicitud del asesor sobre uno de sus clientes.
func (s *Service) Create(ctx context.Context, in CreateInput) (View, error) {
	if err := authz.Require(ctx, authz.QuoteManageOwn); err != nil {
		return View{}, err
	}
	user, err := currentUser(ctx)
	if err != nil {
		return View{}, err
	}
	if err := s.checkVisible(ctx, []uuid.UUID{in.HolderPartyID}, user); err != nil {
		return View{}, err
	}
	// Antes de guardar: el esquema no admite datos de salud, que no pueden quedar en JSONB sin cifrar.
	key, err := s.Lines.RiskSchemaKeyOfLine(ctx, in.LineOfBusinessID)
	if err != nil {
		return View{}, err
	}
	if err := s.Schemas.Validate(key, in.RiskData); err != nil {
		return View{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return View{}, fmt.Errorf("quoting: %w", err)
	}
	r, err := domain.NewRequest(domain.Snapshot{
		ID: id, LeadID: in.LeadID, HolderPartyID: in.HolderPartyID, LineOfBusinessID: in.LineOfBusinessID, AgentID: user,
		RiskDescription: in.RiskDescription, RiskData: in.RiskData,
	})
	if err != nil {
		return View{}, err
	}
	snap := r.Snapshot()
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Requests().Insert(ctx, snap, s.Clock.Now().UTC()); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("quoting.request.created", id))
	})
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, snap)
}
