package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

const (
	expirationBatch      = 500
	expirationMaxBatches = 200
)

// Expirer marca Vencidas las pólizas cuyo último día cubierto ya pasó (job diario). No tiene usuario:
// el actor auditado es "system".
type Expirer struct {
	Store Store
	Clock kernel.Clock
}

// Run procesa todos los tenants y devuelve cuántas pólizas venció. Un error en una póliza no detiene
// las demás; se devuelve al final para que el job reintente, y lo ya hecho no se repite.
func (e *Expirer) Run(ctx context.Context) (int, error) {
	today := kernel.Today(e.Clock)
	var errs []error
	expired := 0
	for range expirationMaxBatches {
		due, err := e.Store.DueForExpiration(ctx, today, expirationBatch)
		if err != nil {
			return expired, errors.Join(append(errs, err)...)
		}
		progress := 0
		for _, d := range due {
			switch err := e.expire(ctx, d, today); {
			case err == nil:
				progress++
			case errors.Is(err, domain.ErrConcurrentUpdate), errors.Is(err, domain.ErrInvalidPolicyTransition):
				// Otro proceso la cambió: ya no le corresponde a este job.
			default:
				errs = append(errs, fmt.Errorf("póliza %s: %w", d.ID, err))
			}
		}
		expired += progress
		if len(due) < expirationBatch || progress == 0 {
			break
		}
	}
	return expired, errors.Join(errs...)
}

func (e *Expirer) expire(ctx context.Context, d DuePolicy, today kernel.Date) error {
	ctx = reqctx.WithTenantID(ctx, d.Tenant)
	return e.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := uow.Policies().Get(ctx, d.ID)
		if err != nil {
			return err
		}
		from := p.Status()
		if err := p.Expire(today, e.Clock.Now()); err != nil {
			return err
		}
		if err := uow.Policies().Save(ctx, p); err != nil {
			return err
		}
		return record(ctx, uow, p, "policies.policy.expired", from, nil)
	})
}
