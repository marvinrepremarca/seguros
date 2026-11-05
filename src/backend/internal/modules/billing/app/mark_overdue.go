package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/billing/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// MarkOverdue avisa las cuotas del tenant del contexto que superaron los días de gracia (job diario).
func MarkOverdue(ctx context.Context, store Store, clock kernel.Clock) error {
	today := kernel.Today(clock)
	var ids []uuid.UUID
	err := store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		ids, err = uow.Billing().PoliciesWithDue(ctx, today)
		return err
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		err := store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
			return markPlanOverdue(ctx, uow, id, today)
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("póliza %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

func markPlanOverdue(ctx context.Context, uow UnitOfWork, policyID uuid.UUID, today kernel.Date) error {
	p, err := uow.Billing().GetPlan(ctx, policyID)
	if err != nil {
		return err
	}
	snap := p.Snapshot()
	grace, err := uow.Billing().GraceDays(ctx, snap.InsurerID)
	if err != nil {
		return err
	}
	due := p.MarkOverdue(today, int(grace))
	if len(due) == 0 {
		return nil
	}
	if err := uow.Billing().SavePlan(ctx, p); err != nil {
		return err
	}
	for _, i := range due {
		if err := uow.Publish(ctx, contracts.TopicInstallmentOverdue, contracts.InstallmentOverdue{PolicyID: policyID, InstallmentID: i.ID, DueDate: i.DueDate.String()}); err != nil {
			return err
		}
	}
	return uow.Audit(ctx, entry("billing.installment.overdue", snap.ID, map[string]string{"count": fmt.Sprint(len(due))}))
}
