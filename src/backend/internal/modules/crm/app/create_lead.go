package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/crm/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// CreateInput son los datos de un prospecto nuevo.
type CreateInput struct {
	Name, Email, Phone, Source string
	LineOfBusinessID, AgentID  uuid.NullUUID
}

// Create registra un prospecto. Quien no puede asignar queda como su asesor; quien puede, elige uno o
// lo deja sin asignar.
func (s *Service) Create(ctx context.Context, in CreateInput) (domain.Snapshot, error) {
	if err := authz.Require(ctx, authz.LeadCreate); err != nil {
		return domain.Snapshot{}, err
	}
	if authz.Require(ctx, authz.LeadAssign) != nil {
		u, err := currentUser(ctx)
		if err != nil {
			return domain.Snapshot{}, err
		}
		in.AgentID = uuid.NullUUID{UUID: u, Valid: true}
	} else if in.AgentID.Valid {
		if err := s.checkAgent(ctx, in.AgentID.UUID); err != nil {
			return domain.Snapshot{}, err
		}
	}
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("crm: %w", err)
	}
	now := s.Clock.Now().UTC()
	lead, err := domain.New(domain.Snapshot{ID: id, Name: in.Name, Email: in.Email, Phone: in.Phone, Source: in.Source, LineOfBusinessID: in.LineOfBusinessID, AgentID: in.AgentID}, now)
	if err != nil {
		return domain.Snapshot{}, err
	}
	snap := lead.Snapshot()
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Leads().Insert(ctx, snap, now); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("crm.lead.created", id))
	})
	return snap, err
}
