package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/modules/billing/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ReminderDates son los vencimientos por recordar hoy: en 5 días y hoy (DECISIONS T1).
func ReminderDates(today kernel.Date) []kernel.Date {
	return []kernel.Date{today.AddDays(5), today}
}

// SendInstallmentReminders publica un evento por cuota pendiente que vence en esas fechas;
// Notifications descarta los repetidos.
func SendInstallmentReminders(ctx context.Context, store Store, clock kernel.Clock) error {
	today := kernel.Today(clock)
	return store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		due, err := uow.Billing().InstallmentsDueOn(ctx, ReminderDates(today))
		if err != nil {
			return err
		}
		for _, d := range due {
			var days int32
			if d.DueDate != today {
				days = 5
			}
			ev := contracts.InstallmentDue{PolicyID: d.PolicyID, InstallmentID: d.InstallmentID, DueDate: d.DueDate.String(), DaysLeft: days}
			if err := uow.Publish(ctx, contracts.TopicInstallmentDue, ev); err != nil {
				return err
			}
		}
		return nil
	})
}
