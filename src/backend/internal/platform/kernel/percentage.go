package kernel

import "github.com/shopspring/decimal"

// percentageMaxScale coincide con las columnas numeric(9,6) (rules/database.md).
const percentageMaxScale = 6

// ErrInvalidPercentage indica un porcentaje fuera de 0 a 100 o con más de 6 decimales.
var ErrInvalidPercentage = NewError(KindValidation, "kernel.percentage.invalid", "porcentaje entre 0 y 100, con hasta 6 decimales")

// Percentage es un porcentaje entre 0 y 100. Inmutable; se pasa por valor.
type Percentage struct {
	value decimal.Decimal
}

// NewPercentage valida rango y escala.
func NewPercentage(value decimal.Decimal) (Percentage, error) {
	if value.IsNegative() || value.GreaterThan(decimal.NewFromInt(100)) || !value.Equal(value.Truncate(percentageMaxScale)) {
		return Percentage{}, ErrInvalidPercentage
	}
	return Percentage{value: value}, nil
}

// Decimal devuelve el valor (12.5 significa 12,5 %).
func (p Percentage) Decimal() decimal.Decimal { return p.value }

// String devuelve el valor en texto, sin el signo %.
func (p Percentage) String() string { return p.value.String() }
