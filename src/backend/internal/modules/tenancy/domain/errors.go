package domain

import "github.com/marvinrepremarca/seguros/internal/platform/kernel"

// Errores de dominio de tenancy (códigos estables).
var (
	ErrInvalidName       = kernel.NewError(kernel.KindValidation, "tenancy.tenant.invalid_name", "nombre vacío o de más de 120 caracteres")
	ErrInvalidPlan       = kernel.NewError(kernel.KindValidation, "tenancy.tenant.invalid_plan", "plan desconocido")
	ErrInvalidOrgAlias   = kernel.NewError(kernel.KindValidation, "tenancy.tenant.invalid_org_alias", "alias de organización inválido")
	ErrInvalidTransition = kernel.NewError(kernel.KindConflict, "tenancy.tenant.invalid_transition", "transición de estado inválida")
	ErrNotFound          = kernel.NewError(kernel.KindNotFound, "tenancy.tenant.not_found", "tenant inexistente")
	ErrOrgAliasTaken     = kernel.NewError(kernel.KindConflict, "tenancy.tenant.org_alias_taken", "la organización ya está vinculada a otro tenant")
	ErrConcurrentUpdate  = kernel.NewError(kernel.KindConflict, "tenancy.tenant.concurrent_update", "el tenant cambió; reintente")
)
