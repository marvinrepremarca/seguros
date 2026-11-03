// Package app contiene los casos de uso de tenancy. Las operaciones de alta, suspensión y cambio
// de plan son de plataforma (super-administrador, DECISIONS.md P2): se invocan desde
// cmd/platformctl y no se exponen por HTTP a los tenants.
package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// TenantRepository persiste el agregado Tenant dentro de una unidad de trabajo.
type TenantRepository interface {
	Insert(ctx context.Context, t *domain.Tenant) error
	// Get devuelve domain.ErrNotFound si no existe (o no es visible bajo RLS).
	Get(ctx context.Context, id kernel.TenantID) (*domain.Tenant, error)
	// Update aplica concurrencia optimista: domain.ErrConcurrentUpdate si la versión cambió.
	Update(ctx context.Context, t *domain.Tenant) error
}

// UnitOfWork agrupa repositorio, auditoría y outbox en una transacción con RLS del tenant.
type UnitOfWork interface {
	Tenants() TenantRepository
	Audit(ctx context.Context, e audit.Entry) error
	// Publish escribe un evento de integración en el outbox de la misma transacción.
	Publish(ctx context.Context, topic string, payload any) error
}

// Store abre unidades de trabajo y resuelve organizaciones.
type Store interface {
	// WithinTenant ejecuta fn en una transacción con RLS para el tenant del contexto.
	WithinTenant(ctx context.Context, fn func(ctx context.Context, uow UnitOfWork) error) error
	// ResolveOrgAlias devuelve el tenant vinculado a la organización (domain.ErrNotFound si no hay).
	ResolveOrgAlias(ctx context.Context, orgAlias string) (kernel.TenantID, error)
}
