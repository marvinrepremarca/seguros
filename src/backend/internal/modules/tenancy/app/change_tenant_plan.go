package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ChangeTenantPlanCommand cambia el plan comercial de una agencia (operación de plataforma).
type ChangeTenantPlanCommand struct {
	OrgAlias string
	Plan     string
	Reason   string
	Operator string
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

// Handle cambia el plan del tenant de la organización. Fijar el plan que ya tiene no escribe nada.
func (h *ChangeTenantPlanHandler) Handle(ctx context.Context, cmd ChangeTenantPlanCommand) error {
	plan, err := domain.ParsePlan(cmd.Plan)
	if err != nil {
		return err
	}
	note, err := domain.NewChangeNote(cmd.Reason, cmd.Operator)
	if err != nil {
		return err
	}
	return applyChange(ctx, h.store, note, cmd.OrgAlias, func(t *domain.Tenant) (*tenantChange, error) {
		previous := t.Plan()
		if previous == plan {
			return nil, nil
		}
		if err := t.ChangePlan(plan, h.clock.Now()); err != nil {
			return nil, err
		}
		return &tenantChange{
			action: "tenancy.tenant.plan_changed", previous: string(previous), next: string(plan),
			topic: contracts.TopicTenantPlanChanged,
			event: contracts.TenantPlanChanged{Previous: string(previous), New: string(plan)},
		}, nil
	})
}
