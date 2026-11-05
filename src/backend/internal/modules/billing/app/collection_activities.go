package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// AddActivity registra una gestión de cobro.
func (s *Service) AddActivity(ctx context.Context, policyID uuid.UUID, channel, note string) (Activity, error) {
	if err := authz.Require(ctx, authz.BillingCollectionManage); err != nil {
		return Activity{}, err
	}
	note, err := cleanText(note, 1000)
	if err != nil {
		return Activity{}, err
	}
	id, err := NewID()
	if err != nil {
		return Activity{}, err
	}
	out := Activity{ID: id, Channel: channel, Note: note, CreatedAt: s.Clock.Now().UTC()}
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := uow.Billing().GetPlan(ctx, policyID)
		if err != nil {
			return err
		}
		planID := p.Snapshot().ID
		if err := uow.Billing().InsertActivity(ctx, planID, out); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("billing.collection.added", planID, map[string]string{"channel": channel}))
	})
	return out, err
}

// ListActivities lista las gestiones de cobro.
func (s *Service) ListActivities(ctx context.Context, policyID uuid.UUID) ([]Activity, error) {
	if err := authz.Require(ctx, authz.BillingPlanRead); err != nil {
		return nil, err
	}
	var out []Activity
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := uow.Billing().GetPlan(ctx, policyID)
		if err != nil {
			return err
		}
		out, err = uow.Billing().ListActivities(ctx, p.Snapshot().ID)
		return err
	})
	return out, err
}
