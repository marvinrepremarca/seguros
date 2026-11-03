// Package domain contiene las reglas de featuremgmt que no dependen del registro de flags: el
// motivo obligatorio de un cambio y los errores de negocio (códigos estables). Las reglas que
// necesitan el registro (dependencias, editabilidad, planes) se aplican en app.
package domain

import (
	"strings"
	"unicode/utf8"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de featuremgmt.
var (
	ErrUnknownFlag          = kernel.NewError(kernel.KindNotFound, "featuremgmt.flag.not_found", "flag no registrado")
	ErrNotTenantEditable    = kernel.NewError(kernel.KindForbidden, "featuremgmt.flag.not_tenant_editable", "el flag solo lo gestiona la plataforma")
	ErrNotInPlan            = kernel.NewError(kernel.KindForbidden, "featuremgmt.flag.not_in_plan", "el plan del tenant no incluye esta funcionalidad")
	ErrGlobalOnly           = kernel.NewError(kernel.KindValidation, "featuremgmt.flag.global_only", "los flags ops son globales y no admiten override por tenant")
	ErrDependencyDisabled   = kernel.NewError(kernel.KindConflict, "featuremgmt.flag.dependency_disabled", "una dependencia del flag está apagada")
	ErrHasEnabledDependents = kernel.NewError(kernel.KindConflict, "featuremgmt.flag.has_enabled_dependents", "hay flags encendidos que dependen de este")
	ErrInvalidReason        = kernel.NewError(kernel.KindValidation, "featuremgmt.flag.invalid_reason", "el motivo debe tener entre 3 y 500 caracteres")
	ErrInvalidOperator      = kernel.NewError(kernel.KindValidation, "featuremgmt.switch.invalid_operator", "el operador es obligatorio (máx. 120 caracteres)")
)

// Reason es el motivo obligatorio de un cambio de flag (queda en la auditoría).
type Reason string

// NewReason valida el motivo.
func NewReason(s string) (Reason, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n < 3 || n > 500 {
		return "", ErrInvalidReason
	}
	return Reason(s), nil
}
