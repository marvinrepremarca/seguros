package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ChangeTenantPlanCommand cambia el plan comercial de una agencia (operación de plataforma).
type ChangeTenantPlanCommand struct {
	OrgAlias string
	Plan     string
}

// ChangeTenantPlanHandler cambia el plan.
type ChangeTenantPlanHandler struct {
	store Store
	clock kernel.Clock
}

// NewChangeTenantPlanHandler construye el handler.
func NewChangeTenantPlanHandler(store Store, clock kernel.Clock) *ChangeTenantPlanHandler {
	return &ChangeTenantPlanHandler{store: store, clock: clock}
}

// Handle cambia el plan del tenant de la organización.
func (h *ChangeTenantPlanHandler) Handle(ctx context.Context, cmd ChangeTenantPlanCommand) error {
	plan, err := domain.ParsePlan(cmd.Plan)
	if err != nil {
		return err
	}
	return updateByOrgAlias(ctx, h.store, cmd.OrgAlias, func(t *domain.Tenant) error {
		return t.ChangePlan(plan, h.clock.Now())
	})
}
