// Package contracts es la única superficie pública del módulo tenancy.
package contracts

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ErrTenantNotFound indica que no hay tenant para la organización o el ID dados.
var ErrTenantNotFound = kernel.NewError(kernel.KindNotFound, "tenancy.tenant.not_found", "tenant inexistente")

// TenantInfo es la vista mínima de un tenant que necesitan la plataforma y otros módulos.
type TenantInfo struct {
	ID     kernel.TenantID
	Plan   string // basico | profesional | empresarial
	Active bool
}

// Directory resuelve tenants. Lo implementa tenancy y se inyecta en cmd/api.
type Directory interface {
	// ResolveByOrgAlias traduce la Organization de Keycloak del usuario (login) a su tenant.
	// Devuelve ErrTenantNotFound si la organización no está vinculada.
	ResolveByOrgAlias(ctx context.Context, orgAlias string) (TenantInfo, error)
	// Get devuelve el tenant indicado. Consulta bajo RLS con ese mismo tenant: nunca expone otro.
	Get(ctx context.Context, id kernel.TenantID) (TenantInfo, error)
}
