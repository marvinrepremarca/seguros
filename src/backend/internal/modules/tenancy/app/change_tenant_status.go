package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// ChangeTenantStatusCommand suspende o reactiva una agencia (operación de plataforma).
type ChangeTenantStatusCommand struct {
	OrgAlias string
	Suspend  bool
}

// ChangeTenantStatusHandler aplica la transición de estado.
type ChangeTenantStatusHandler struct {
	store Store
	clock kernel.Clock
}

// NewChangeTenantStatusHandler construye el handler.
func NewChangeTenantStatusHandler(store Store, clock kernel.Clock) *ChangeTenantStatusHandler {
	return &ChangeTenantStatusHandler{store: store, clock: clock}
}

// Handle suspende (Suspend=true) o reactiva el tenant de la organización.
func (h *ChangeTenantStatusHandler) Handle(ctx context.Context, cmd ChangeTenantStatusCommand) error {
	return updateByOrgAlias(ctx, h.store, cmd.OrgAlias, func(t *domain.Tenant) error {
		if cmd.Suspend {
			return t.Suspend(h.clock.Now())
		}
		return t.Reactivate(h.clock.Now())
	})
}

// updateByOrgAlias carga el tenant de la organización, aplica change y lo guarda en una
// transacción bajo RLS de ese tenant.
func updateByOrgAlias(ctx context.Context, store Store, orgAlias string, change func(*domain.Tenant) error) error {
	id, err := store.ResolveOrgAlias(ctx, orgAlias)
	if err != nil {
		return err
	}
	ctx = reqctx.WithTenantID(ctx, id)
	return store.WithinTenant(ctx, func(ctx context.Context, repo TenantRepository) error {
		t, err := repo.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := change(t); err != nil {
			return err
		}
		return repo.Update(ctx, t)
	})
}
