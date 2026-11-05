// Package billingevents contiene los consumidores de eventos de billing.
package billingevents

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/marvinrepremarca/seguros/internal/modules/billing/app"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

// Binder da la unidad de trabajo sobre la transacción del consumidor, que incluye el inbox.
type Binder interface {
	Bind(ctx context.Context, tx pgx.Tx) (app.UnitOfWork, error)
}

func consumer[E any](store Binder, name, topic string, handle func(context.Context, app.UnitOfWork, E) error) outbox.Consumer {
	return outbox.Consumer{
		Name: name, Topic: topic, Flag: flags.BillingModule,
		Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
			var ev E
			if err := e.Decode(&ev); err != nil {
				return err
			}
			uow, err := store.Bind(ctx, tx)
			if err != nil {
				return err
			}
			return handle(ctx, uow, ev)
		},
	}
}

// Consumers devuelve los consumidores de billing.
func Consumers(store Binder, clock kernel.Clock) []outbox.Consumer {
	r := app.Reactor{Clock: clock}
	return []outbox.Consumer{
		consumer(store, "billing.create-plan", policies.TopicPolicyIssued, app.OnPolicyIssued),
		consumer(store, "billing.apply-endorsement", policies.TopicPolicyEndorsed, r.OnPolicyEndorsed),
		consumer(store, "billing.cancel-plan", policies.TopicPolicyCancelled, app.OnPolicyCancelled),
	}
}
