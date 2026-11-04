package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// ChangeTenantStatusCommand suspende o reactiva una agencia (operación de plataforma).
type ChangeTenantStatusCommand struct {
	OrgAlias string
	Suspend  bool
	Reason   string
	Operator string
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
	note, err := domain.NewChangeNote(cmd.Reason, cmd.Operator)
	if err != nil {
		return err
	}
	return applyChange(ctx, h.store, note, cmd.OrgAlias, func(t *domain.Tenant) (*tenantChange, error) {
		previous, action := t.Status(), "tenancy.tenant.reactivated"
		if cmd.Suspend {
			action = "tenancy.tenant.suspended"
			err = t.Suspend(h.clock.Now())
		} else {
			err = t.Reactivate(h.clock.Now())
		}
		if err != nil {
			return nil, err
		}
		return &tenantChange{
			action: action, previous: string(previous), next: string(t.Status()),
			topic: contracts.TopicTenantStatusChanged,
			event: contracts.TenantStatusChanged{Previous: string(previous), New: string(t.Status())},
		}, nil
	})
}

// tenantChange describe lo que se audita y se publica tras cambiar un tenant.
type tenantChange struct {
	action   string
	previous string
	next     string
	topic    string
	event    any
}

// applyChange carga el tenant de la organización, aplica change y guarda, audita y publica en una
// sola transacción bajo RLS de ese tenant. Si change devuelve nil no hubo cambio y no se escribe.
func applyChange(ctx context.Context, store Store, note domain.ChangeNote, orgAlias string, change func(*domain.Tenant) (*tenantChange, error)) error {
	id, err := store.ResolveOrgAlias(ctx, orgAlias)
	if err != nil {
		return err
	}
	ctx = reqctx.WithTenantID(ctx, id)
	return store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		t, err := uow.Tenants().Get(ctx, id)
		if err != nil {
			return err
		}
		c, err := change(t)
		if err != nil || c == nil {
			return err
		}
		if err := uow.Tenants().Update(ctx, t); err != nil {
			return err
		}
		return record(ctx, uow, t.ID(), note, c)
	})
}

// record audita y publica el cambio dentro de la unidad de trabajo.
func record(ctx context.Context, uow UnitOfWork, id kernel.TenantID, note domain.ChangeNote, c *tenantChange) error {
	if err := uow.Audit(ctx, audit.Entry{
		Action: c.action, ResourceType: "tenancy.tenant", ResourceID: id.String(), Outcome: audit.OutcomeSuccess,
		Details: map[string]string{
			"previous": c.previous, "new": c.next, "reason": note.Reason(), "operator": note.Operator(), "actor": "platform",
		},
	}); err != nil {
		return err
	}
	return uow.Publish(ctx, c.topic, c.event)
}
