// Package commissionsevents contiene los consumidores de comisiones.
package commissionsevents

import (
	"context"

	"github.com/jackc/pgx/v5"

	billing "github.com/marvinrepremarca/seguros/internal/modules/billing/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/commissions/app"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

// Binder da el repositorio sobre la transacción del consumidor, que incluye el inbox.
type Binder interface {
	Bind(ctx context.Context, tx pgx.Tx) (app.Repository, error)
}

func consumer[E any](store Binder, name, topic string, handle func(context.Context, app.Repository, E) error) outbox.Consumer {
	return outbox.Consumer{
		Name: name, Topic: topic, Flag: flags.CommissionsModule,
		Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
			var ev E
			if err := e.Decode(&ev); err != nil {
				return err
			}
			repo, err := store.Bind(ctx, tx)
			if err != nil {
				return err
			}
			return handle(ctx, repo, ev)
		},
	}
}

// Consumers devuelve los consumidores de comisiones.
func Consumers(store Binder, r *app.Reactor) []outbox.Consumer {
	return []outbox.Consumer{
		consumer(store, "commissions.policy-issued", policies.TopicPolicyIssued, r.OnPolicyIssued),
		consumer(store, "commissions.policy-endorsed", policies.TopicPolicyEndorsed, r.OnPolicyEndorsed),
		consumer(store, "commissions.policy-cancelled", policies.TopicPolicyCancelled, r.OnPolicyCancelled),
		consumer(store, "commissions.agent-changed", policies.TopicPolicyAgentChanged, r.OnAgentChanged),
		consumer(store, "commissions.payment-applied", billing.TopicPaymentApplied, r.OnPaymentApplied),
		consumer(store, "commissions.payment-reversed", billing.TopicPaymentReversed, r.OnPaymentReversed),
	}
}
