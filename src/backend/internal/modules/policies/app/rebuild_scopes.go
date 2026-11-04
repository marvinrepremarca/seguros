package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
)

// rebuildBatch es cuántas pólizas se republican por transacción.
const rebuildBatch = 200

// ScopeRebuilder vuelve a publicar el alcance de las pólizas del tenant del contexto, para construir o
// reparar la proyección parties.policy_scopes (despliegue inicial o un evento perdido).
type ScopeRebuilder struct {
	Store Store
}

// Run devuelve cuántas pólizas republicó.
func (r *ScopeRebuilder) Run(ctx context.Context) (int, error) {
	total := 0
	after := uuid.NullUUID{}
	for {
		var ids []domain.PolicyID
		err := r.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
			var err error
			if ids, err = uow.Policies().IDsAfter(ctx, after, rebuildBatch); err != nil {
				return err
			}
			for _, id := range ids {
				p, err := uow.Policies().Get(ctx, id)
				if err != nil {
					return err
				}
				if err := publish(ctx, uow, []domain.Event{p.CurrentScope()}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return total, err
		}
		total += len(ids)
		if len(ids) < rebuildBatch {
			return total, nil
		}
		after = uuid.NullUUID{UUID: uuid.UUID(ids[len(ids)-1]), Valid: true}
	}
}
