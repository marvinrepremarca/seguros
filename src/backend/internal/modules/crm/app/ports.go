// Package app contiene los casos de uso de crm.
package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/crm/domain"
	identity "github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Repository persiste prospectos del tenant del contexto.
type Repository interface {
	Insert(ctx context.Context, s domain.Snapshot, at time.Time) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Lead, error)
	Save(ctx context.Context, l *domain.Lead, at time.Time) error
	List(ctx context.Context, status string, agent, before uuid.NullUUID, limit int32) ([]domain.Snapshot, error)
}

// UnitOfWork es una transacción del tenant.
type UnitOfWork interface {
	Leads() Repository
	Audit(ctx context.Context, e audit.Entry) error
}

// Store abre unidades de trabajo.
type Store interface {
	WithinTenant(ctx context.Context, fn func(ctx context.Context, uow UnitOfWork) error) error
}

// Service agrupa los casos de uso.
type Service struct {
	Store Store
	Scope parties.AgentScope
	Users identity.Directory
	Clock kernel.Clock
}

func entry(action string, id uuid.UUID) audit.Entry {
	return audit.Entry{Action: action, ResourceType: "crm.lead", ResourceID: id.String(), Outcome: audit.OutcomeSuccess}
}

func currentUser(ctx context.Context) (uuid.UUID, error) {
	u, ok := reqctx.UserID(ctx)
	if !ok {
		return uuid.Nil, authz.ErrUnauthenticated
	}
	return uuid.UUID(u), nil
}

// readScope devuelve el asesor que limita la lectura; sin valor, ve todos.
func readScope(ctx context.Context) (uuid.NullUUID, error) {
	err := authz.Require(ctx, authz.LeadRead)
	if err == nil {
		return uuid.NullUUID{}, nil
	}
	if !errors.Is(err, authz.ErrForbidden) {
		return uuid.NullUUID{}, err
	}
	if err := authz.Require(ctx, authz.LeadReadOwn); err != nil {
		return uuid.NullUUID{}, err
	}
	u, err := currentUser(ctx)
	if err != nil {
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: u, Valid: true}, nil
}

func (s *Service) checkAgent(ctx context.Context, agent uuid.UUID) error {
	u, ok, err := s.Users.Agent(ctx, agent)
	if err != nil {
		return fmt.Errorf("crm: consultando asesor: %w", err)
	}
	if !ok || !u.Active {
		return domain.ErrInvalidAgent
	}
	return nil
}
