// Package domain contiene los cálculos de los indicadores (DOMAIN §5).
package domain

import "github.com/marvinrepremarca/seguros/internal/platform/kernel"

// MaxRangeDays limita el periodo consultado.
const MaxRangeDays = 1830

// ErrInvalidRange indica un periodo no válido.
var ErrInvalidRange = kernel.NewError(kernel.KindValidation, "reporting.range.invalid", "el periodo no es válido")

// ValidateRange exige un periodo ordenado y acotado.
func ValidateRange(from, to kernel.Date) error {
	if to.Before(from) || to.After(from.AddDays(MaxRangeDays)) {
		return ErrInvalidRange
	}
	return nil
}
