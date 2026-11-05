// Package renewalsevents contiene los consumidores de renovaciones.
package renewalsevents

import (
	"context"

	"github.com/jackc/pgx/v5"

	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals/app"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

// Binder da la unidad de trabajo sobre la transacción del consumidor, que incluye el inbox.
type Binder interface {
	Bind(ctx context.Context, tx pgx.Tx) (app.UnitOfWork, error)
}

func consumer[E any](store Binder, clock kernel.Clock, name, topic string, handle func(context.Context, app.UnitOfWork, kernel.Clock, E) error) outbox.Consumer {
	return outbox.Consumer{
		Name: name, Topic: topic, Flag: flags.RenewalsModule,
		Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
			var ev E
			if err := e.Decode(&ev); err != nil {
				return err
			}
			uow, err := store.Bind(ctx, tx)
			if err != nil {
				return err
			}
			return handle(ctx, uow, clock, ev)
		},
	}
}

// Consumers devuelve los consumidores de renovaciones.
func Consumers(store Binder, clock kernel.Clock) []outbox.Consumer {
	return []outbox.Consumer{
		consumer(store, clock, "renewals.policy-issued", policies.TopicPolicyIssued, app.OnPolicyIssued),
		consumer(store, clock, "renewals.policy-cancelled", policies.TopicPolicyCancelled, app.OnPolicyCancelled),
		consumer(store, clock, "renewals.policy-expired", policies.TopicPolicyExpired, app.OnPolicyExpired),
	}
}
