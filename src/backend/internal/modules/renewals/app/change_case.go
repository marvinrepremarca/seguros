package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/renewals/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

func (s *Service) change(ctx context.Context, policyID uuid.UUID, action string, fn func(context.Context, UnitOfWork, *domain.Case) error) (View, error) {
	if err := authz.Require(ctx, authz.RenewalManage); err != nil {
		return View{}, err
	}
	var snap domain.Snapshot
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		c, err := uow.Renewals().GetCase(ctx, policyID)
		if err != nil {
			return err
		}
		if err := fn(ctx, uow, c); err != nil {
			return err
		}
		if err := uow.Renewals().SaveCase(ctx, c, s.Clock.Now().UTC()); err != nil {
			return err
		}
		snap = c.Snapshot()
		return uow.Audit(ctx, entry(action, snap.ID))
	})
	if err != nil {
		return View{}, err
	}
	out, err := s.views(ctx, []domain.Snapshot{snap})
	if err != nil {
		return View{}, err
	}
	return out[0], nil
}

// Start inicia la gestión de la renovación.
func (s *Service) Start(ctx context.Context, policyID uuid.UUID) (View, error) {
	return s.change(ctx, policyID, "renewals.case.started", func(_ context.Context, _ UnitOfWork, c *domain.Case) error { return c.Start() })
}

// MarkLost registra que el cliente no renueva. Policies cierra la póliza cuando venza.
func (s *Service) MarkLost(ctx context.Context, policyID uuid.UUID, reason string) (View, error) {
	return s.change(ctx, policyID, "renewals.case.lost", func(ctx context.Context, uow UnitOfWork, c *domain.Case) error {
		if err := c.MarkLost(reason); err != nil {
			return err
		}
		return uow.Publish(ctx, contracts.TopicRenewalLost, contracts.RenewalLost{PolicyID: policyID})
	})
}
