// Package app contiene los casos de uso de cotización.
package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	catalog "github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	identity "github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/quoting/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Repository persiste solicitudes del tenant del contexto.
type Repository interface {
	Insert(ctx context.Context, s domain.Snapshot, at time.Time) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Request, error)
	Save(ctx context.Context, r *domain.Request, at time.Time) error
	InsertOffer(ctx context.Context, request uuid.UUID, o domain.Offer, at time.Time) error
	List(ctx context.Context, status string, agent, before uuid.NullUUID, limit int32) ([]domain.Snapshot, error)
	OfferValidDays(ctx context.Context) (int32, error)
	SetOfferValidDays(ctx context.Context, days int32, at time.Time) error
}

// UnitOfWork es una transacción del tenant.
type UnitOfWork interface {
	Requests() Repository
	Audit(ctx context.Context, e audit.Entry) error
	Publish(ctx context.Context, topic string, payload any) error
}

// Store abre unidades de trabajo.
type Store interface {
	WithinTenant(ctx context.Context, fn func(ctx context.Context, uow UnitOfWork) error) error
}

// Service agrupa los casos de uso.
type Service struct {
	Store   Store
	Scope   parties.AgentScope
	Parties parties.Directory
	Users   identity.Directory
	Catalog catalog.Directory
	Schemas catalog.RiskSchemas
	Lines   catalog.LineSchemas
	Clock   kernel.Clock
}

// View es una solicitud con el nombre del tomador.
type View struct {
	domain.Snapshot
	HolderName string
}

func entry(action string, id uuid.UUID) audit.Entry {
	return audit.Entry{Action: action, ResourceType: "quoting.request", ResourceID: id.String(), Outcome: audit.OutcomeSuccess}
}

func currentUser(ctx context.Context) (uuid.UUID, error) {
	u, ok := reqctx.UserID(ctx)
	if !ok {
		return uuid.Nil, authz.ErrUnauthenticated
	}
	return uuid.UUID(u), nil
}

func readScope(ctx context.Context) (uuid.NullUUID, error) {
	err := authz.Require(ctx, authz.QuoteRead)
	if err == nil {
		return uuid.NullUUID{}, nil
	}
	if !errors.Is(err, authz.ErrForbidden) {
		return uuid.NullUUID{}, err
	}
	if err := authz.Require(ctx, authz.QuoteReadOwn); err != nil {
		return uuid.NullUUID{}, err
	}
	u, err := currentUser(ctx)
	if err != nil {
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: u, Valid: true}, nil
}

func (s *Service) views(ctx context.Context, rows []domain.Snapshot) ([]View, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.HolderPartyID)
	}
	names, err := s.Parties.Summaries(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("quoting: consultando tomadores: %w", err)
	}
	out := make([]View, 0, len(rows))
	for _, r := range rows {
		out = append(out, View{Snapshot: r, HolderName: names[r.HolderPartyID].Name})
	}
	return out, nil
}

func (s *Service) view(ctx context.Context, snap domain.Snapshot) (View, error) {
	out, err := s.views(ctx, []domain.Snapshot{snap})
	if err != nil {
		return View{}, err
	}
	return out[0], nil
}

// loadOwn carga la solicitud del asesor; la de otro responde como inexistente.
func loadOwn(ctx context.Context, uow UnitOfWork, id, user uuid.UUID) (*domain.Request, error) {
	r, err := uow.Requests().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if r.Snapshot().AgentID != user {
		return nil, domain.ErrNotFound
	}
	return r, nil
}

func (s *Service) checkVisible(ctx context.Context, parties []uuid.UUID, agent uuid.UUID) error {
	for _, p := range parties {
		ok, err := s.Scope.VisibleToAgent(ctx, p, agent)
		if err != nil {
			return fmt.Errorf("quoting: consultando cliente: %w", err)
		}
		if !ok {
			return domain.ErrInvalidParty
		}
	}
	return nil
}
