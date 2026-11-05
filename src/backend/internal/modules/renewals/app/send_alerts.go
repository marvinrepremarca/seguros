package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/modules/renewals/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// SendAlerts publica los umbrales cruzados del tenant del contexto (DECISIONS T2). Las alertas se
// marcan en la misma transacción: repetirlo no duplica avisos.
func SendAlerts(ctx context.Context, store Store, clock kernel.Clock) error {
	today := kernel.Today(clock)
	now := clock.Now().UTC()
	return store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		repo := uow.Renewals()
		thresholds, err := repo.AlertDays(ctx)
		if err != nil {
			return err
		}
		var longest int32
		for _, t := range thresholds {
			longest = max(longest, t)
		}
		due, err := repo.OpenCasesUntil(ctx, today, today.AddDays(int(longest)))
		if err != nil {
			return err
		}
		for _, c := range due {
			sent, err := repo.SentAlerts(ctx, c.PolicyID)
			if err != nil {
				return err
			}
			left := int32(today.DaysUntil(c.EffectiveTo)) //nolint:gosec // acotado por el umbral
			for _, t := range domain.DueAlerts(thresholds, sent, left) {
				if err := repo.InsertAlert(ctx, c.PolicyID, t, now); err != nil {
					return err
				}
				if err := uow.Publish(ctx, contracts.TopicPolicyExpiring, contracts.PolicyExpiring{PolicyID: c.PolicyID, DaysLeft: left}); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
