// Package contracts es la única superficie pública del módulo catalog.
package contracts

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores públicos del catálogo.
var (
	ErrInvalidReference  = kernel.NewError(kernel.KindValidation, "catalog.reference.invalid", "aseguradora, ramo o producto inexistente, inactivo o incoherente")
	ErrUnknownRiskSchema = kernel.NewError(kernel.KindValidation, "catalog.risk_schema.unknown", "esquema de riesgo desconocido")
	ErrInvalidRiskData   = kernel.NewError(kernel.KindValidation, "catalog.risk_data.invalid", "datos de riesgo inválidos para el ramo")
)

// RiskSchema es un esquema de datos de riesgo publicado.
type RiskSchema struct {
	Key     string
	Version int32
	Label   string
	Schema  map[string]any
}

// RiskSchemas valida datos de riesgo contra los esquemas vigentes.
type RiskSchemas interface {
	List() []RiskSchema
	// Current devuelve la versión vigente del esquema.
	Current(key string) (int32, bool)
	// Validate devuelve ErrInvalidRiskData o ErrUnknownRiskSchema; nunca incluye los valores.
	Validate(key string, data []byte) error
}

// PolicyRef son las referencias del catálogo que usa una póliza.
type PolicyRef struct {
	InsurerID        uuid.UUID
	LineOfBusinessID uuid.UUID
	ProductID        uuid.NullUUID
}

// Directory consulta el catálogo del tenant del contexto para otros módulos.
type Directory interface {
	// ResolvePolicyRef verifica que las referencias existan, estén activas y sean coherentes (el
	// producto es de esa aseguradora y ese ramo). Devuelve la clave del esquema de riesgo del ramo o
	// ErrInvalidReference.
	ResolvePolicyRef(ctx context.Context, ref PolicyRef) (riskSchemaKey string, err error)
}
