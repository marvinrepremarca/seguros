// Package notificationsevents convierte eventos en avisos pendientes.
package notificationsevents

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	billing "github.com/marvinrepremarca/seguros/internal/modules/billing/contracts"
	claims "github.com/marvinrepremarca/seguros/internal/modules/claims/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/notifications/app"
	"github.com/marvinrepremarca/seguros/internal/modules/notifications/domain"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	renewals "github.com/marvinrepremarca/seguros/internal/modules/renewals/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

// Binder da la unidad de trabajo sobre la transacción del consumidor, que incluye el inbox.
type Binder interface {
	Bind(ctx context.Context, tx pgx.Tx) (app.UnitOfWork, error)
}

type notice struct {
	template string
	policy   uuid.UUID
	params   domain.Params
	key      string // vacío: basta el ID del evento
}

func consumer[E any](store Binder, clock kernel.Clock, name, topic string, build func(E) notice) outbox.Consumer {
	return outbox.Consumer{
		Name: name, Topic: topic, Flag: flags.NotificationsModule,
		Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
			var ev E
			if err := e.Decode(&ev); err != nil {
				return err
			}
			uow, err := store.Bind(ctx, tx)
			if err != nil {
				return err
			}
			n := build(ev)
			key := n.key
			if key == "" {
				key = e.ID.String()
			}
			return app.Enqueue(ctx, uow, clock, n.template, n.policy, n.params, n.template+":"+key)
		},
	}
}

// Consumers devuelve los consumidores de notificaciones (DOMAIN §3).
func Consumers(store Binder, clock kernel.Clock) []outbox.Consumer {
	return []outbox.Consumer{
		consumer(store, clock, "notifications.policy-issued", policies.TopicPolicyIssued, func(e policies.PolicyIssued) notice {
			return notice{template: domain.TplPolicyIssued, policy: e.PolicyID}
		}),
		consumer(store, clock, "notifications.policy-arrears", policies.TopicPolicyFellIntoArrears, func(e policies.PolicyFellIntoArrears) notice {
			return notice{template: domain.TplArrears, policy: e.PolicyID}
		}),
		consumer(store, clock, "notifications.policy-reinstated", policies.TopicPolicyReinstated, func(e policies.PolicyReinstated) notice {
			return notice{template: domain.TplReinstated, policy: e.PolicyID}
		}),
		consumer(store, clock, "notifications.policy-expiring", renewals.TopicPolicyExpiring, func(e renewals.PolicyExpiring) notice {
			return notice{template: domain.TplExpiring, policy: e.PolicyID, params: domain.Params{DaysLeft: e.DaysLeft}}
		}),
		consumer(store, clock, "notifications.installment-overdue", billing.TopicInstallmentOverdue, func(e billing.InstallmentOverdue) notice {
			return notice{template: domain.TplOverdue, policy: e.PolicyID, params: domain.Params{DueDate: e.DueDate}, key: e.InstallmentID.String() + ":" + e.DueDate}
		}),
		consumer(store, clock, "notifications.installment-due", billing.TopicInstallmentDue, func(e billing.InstallmentDue) notice {
			tpl := domain.TplDueSoon
			if e.DaysLeft == 0 {
				tpl = domain.TplDueToday
			}
			return notice{template: tpl, policy: e.PolicyID, params: domain.Params{DueDate: e.DueDate}, key: e.InstallmentID.String() + ":" + e.DueDate}
		}),
		consumer(store, clock, "notifications.claim-reported", claims.TopicClaimReported, func(e claims.ClaimReported) notice {
			return notice{template: domain.TplClaimReported, policy: e.PolicyID}
		}),
		consumer(store, clock, "notifications.claim-status", claims.TopicClaimStatusChanged, func(e claims.ClaimStatusChanged) notice {
			return notice{template: domain.TplClaimStatus, policy: e.PolicyID, params: domain.Params{Status: e.To}}
		}),
	}
}
