package app

import (
	"context"
	"fmt"

	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// ProvisionTenantCommand da de alta una agencia (operación de plataforma).
type ProvisionTenantCommand struct {
	Name     string
	Plan     string
	OrgAlias string
	Reason   string
	Operator string
}

// ProvisionTenantHandler crea el tenant activo vinculado a su Organization de Keycloak.
type ProvisionTenantHandler struct {
	store Store
	clock kernel.Clock
}

// NewProvisionTenantHandler construye el handler.
func NewProvisionTenantHandler(store Store, clock kernel.Clock) *ProvisionTenantHandler {
	return &ProvisionTenantHandler{store: store, clock: clock}
}

// Handle crea el tenant y devuelve su ID.
func (h *ProvisionTenantHandler) Handle(ctx context.Context, cmd ProvisionTenantCommand) (kernel.TenantID, error) {
	plan, err := domain.ParsePlan(cmd.Plan)
	if err != nil {
		return kernel.TenantID{}, err
	}
	note, err := domain.NewChangeNote(cmd.Reason, cmd.Operator)
	if err != nil {
		return kernel.TenantID{}, err
	}
	id, err := kernel.NewID[kernel.TenantID]()
	if err != nil {
		return kernel.TenantID{}, fmt.Errorf("tenancy: %w", err)
	}
	tenant, err := domain.NewTenant(id, cmd.Name, plan, cmd.OrgAlias, h.clock.Now())
	if err != nil {
		return kernel.TenantID{}, err
	}
	// La fila nueva solo pasa el WITH CHECK de RLS dentro del contexto de su propio tenant.
	ctx = reqctx.WithTenantID(ctx, id)
	if err := h.store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Tenants().Insert(ctx, tenant); err != nil {
			return err
		}
		return record(ctx, uow, id, note, &tenantChange{
			action: "tenancy.tenant.provisioned", next: string(tenant.Status()),
			topic: contracts.TopicTenantStatusChanged,
			event: contracts.TenantStatusChanged{New: string(tenant.Status())},
		})
	}); err != nil {
		return kernel.TenantID{}, err
	}
	return id, nil
}
