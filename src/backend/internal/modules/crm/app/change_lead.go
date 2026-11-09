package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/crm/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// change gestiona un prospecto propio: el de otro asesor responde 404.
func (s *Service) change(ctx context.Context, id uuid.UUID, action string, fn func(*domain.Lead, time.Time) error) (domain.Snapshot, error) {
	if err := authz.Require(ctx, authz.LeadManageOwn); err != nil {
		return domain.Snapshot{}, err
	}
	user, err := currentUser(ctx)
	if err != nil {
		return domain.Snapshot{}, err
	}
	var snap domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		l, err := uow.Leads().Get(ctx, id)
		if err != nil {
			return err
		}
		if a := l.Snapshot().AgentID; !a.Valid || a.UUID != user {
			return domain.ErrNotFound
		}
		now := s.Clock.Now().UTC()
		if err := fn(l, now); err != nil {
			return err
		}
		if err := uow.Leads().Save(ctx, l, now); err != nil {
			return err
		}
		snap = l.Snapshot()
		return uow.Audit(ctx, entry(action, id))
	})
	return snap, err
}

// MarkContacted registra el contacto.
func (s *Service) MarkContacted(ctx context.Context, id uuid.UUID) (domain.Snapshot, error) {
	return s.change(ctx, id, "crm.lead.contacted", func(l *domain.Lead, now time.Time) error { return l.MarkContacted(now) })
}

// MarkInterested registra el interés.
func (s *Service) MarkInterested(ctx context.Context, id uuid.UUID) (domain.Snapshot, error) {
	return s.change(ctx, id, "crm.lead.interested", func(l *domain.Lead, _ time.Time) error { return l.MarkInterested() })
}

// ScheduleFollowUp programa el seguimiento.
func (s *Service) ScheduleFollowUp(ctx context.Context, id uuid.UUID, on kernel.Date) (domain.Snapshot, error) {
	return s.change(ctx, id, "crm.lead.follow_up", func(l *domain.Lead, _ time.Time) error { return l.ScheduleFollowUp(on, kernel.Today(s.Clock)) })
}

// MarkLost cierra el prospecto. El motivo es texto libre: no va a la bitácora.
func (s *Service) MarkLost(ctx context.Context, id uuid.UUID, reason string) (domain.Snapshot, error) {
	return s.change(ctx, id, "crm.lead.lost", func(l *domain.Lead, _ time.Time) error { return l.MarkLost(reason) })
}

// StartQuoting enlaza el cliente, que debe estar al alcance del asesor (DECISIONS N10, N27).
func (s *Service) StartQuoting(ctx context.Context, id, party uuid.UUID) (domain.Snapshot, error) {
	if err := authz.Require(ctx, authz.LeadManageOwn); err != nil {
		return domain.Snapshot{}, err
	}
	user, err := currentUser(ctx)
	if err != nil {
		return domain.Snapshot{}, err
	}
	visible, err := s.Scope.VisibleToAgent(ctx, party, user)
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("crm: consultando cliente: %w", err)
	}
	if !visible {
		return domain.Snapshot{}, domain.ErrInvalidParty
	}
	return s.change(ctx, id, "crm.lead.quoting", func(l *domain.Lead, _ time.Time) error { return l.StartQuoting(party) })
}

// Assign cambia el asesor de un prospecto no terminado.
func (s *Service) Assign(ctx context.Context, id, agent uuid.UUID) (domain.Snapshot, error) {
	if err := authz.Require(ctx, authz.LeadAssign); err != nil {
		return domain.Snapshot{}, err
	}
	if err := s.checkAgent(ctx, agent); err != nil {
		return domain.Snapshot{}, err
	}
	var snap domain.Snapshot
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		l, err := uow.Leads().Get(ctx, id)
		if err != nil {
			return err
		}
		now := s.Clock.Now().UTC()
		if err := l.Assign(agent, now); err != nil {
			return err
		}
		if err := uow.Leads().Save(ctx, l, now); err != nil {
			return err
		}
		snap = l.Snapshot()
		e := entry("crm.lead.assigned", id)
		e.Details = map[string]string{"new": agent.String()}
		return uow.Audit(ctx, e)
	})
	return snap, err
}

// MarkWon procesa QuoteAccepted (DECISIONS N28). Exige que coincidan asesor y cliente: el leadId de la
// solicitud lo envió el cliente HTTP y Quoting no puede verificarlo. Lo que no aplica no cambia nada.
func MarkWon(ctx context.Context, uow UnitOfWork, clock kernel.Clock, id uuid.UUID, agent *uuid.UUID, party uuid.UUID) error {
	l, err := uow.Leads().Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	s := l.Snapshot()
	if agent == nil || !s.AgentID.Valid || s.AgentID.UUID != *agent || !s.PartyID.Valid || s.PartyID.UUID != party {
		return nil
	}
	if err := l.MarkWon(); err != nil {
		if errors.Is(err, domain.ErrInvalidTransition) {
			return nil
		}
		return err
	}
	if err := uow.Leads().Save(ctx, l, clock.Now().UTC()); err != nil {
		return err
	}
	return uow.Audit(ctx, entry("crm.lead.won", id))
}
