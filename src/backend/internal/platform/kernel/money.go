package kernel

import (
	"log/slog"

	"github.com/shopspring/decimal"
)

// Currency es un código ISO 4217. Solo se aceptan las monedas listadas en MinorUnits.
type Currency string

// Monedas soportadas. COP es la moneda por defecto del negocio (DECISIONS P3).
const (
	COP Currency = "COP"
	USD Currency = "USD"
	EUR Currency = "EUR"
)

// MinorUnits devuelve los decimales de la unidad menor según ISO 4217 y si la moneda es soportada.
func (c Currency) MinorUnits() (int32, bool) {
	switch c {
	case COP, USD, EUR:
		return 2, true
	default:
		return 0, false
	}
}

// minorUnitsOrZero se usa donde la moneda ya fue validada al construir Money.
func (c Currency) minorUnitsOrZero() int32 {
	if places, ok := c.MinorUnits(); ok {
		return places
	}
	return 0
}

// Límites de almacenamiento: las columnas de dinero son numeric(19,4) (rules/database.md).
const (
	MoneyMaxScale       = 4
	moneyMaxIntegerDigs = 15
)

// Errores de Money.
var (
	ErrCurrencyMismatch    = NewError(KindValidation, "kernel.money.currency_mismatch", "operación entre monedas distintas")
	ErrUnsupportedCurrency = NewError(KindValidation, "kernel.money.unsupported_currency", "moneda no soportada")
	ErrMoneyScale          = NewError(KindValidation, "kernel.money.scale", "el monto excede 4 decimales")
	ErrMoneyOverflow       = NewError(KindValidation, "kernel.money.overflow", "el monto excede numeric(19,4)")
	ErrInvalidAmount       = NewError(KindValidation, "kernel.money.invalid_amount", "monto inválido")
	ErrInvalidAllocation   = NewError(KindValidation, "kernel.money.invalid_allocation", "número de partes inválido")
)

// Money es un monto en una moneda. Inmutable; se pasa por valor. El valor cero no es válido
// (moneda vacía): créalo con NewMoney, ParseMoney o Zero.
type Money struct {
	amount   decimal.Decimal
	currency Currency
}

// NewMoney valida moneda, escala (≤ 4 decimales) y rango de numeric(19,4).
func NewMoney(amount decimal.Decimal, currency Currency) (Money, error) {
	if _, ok := currency.MinorUnits(); !ok {
		return Money{}, ErrUnsupportedCurrency.WithMessage("moneda no soportada: %q", string(currency))
	}
	if !amount.Equal(amount.Truncate(MoneyMaxScale)) {
		return Money{}, ErrMoneyScale
	}
	if err := checkRange(amount); err != nil {
		return Money{}, err
	}
	return Money{amount: amount, currency: currency}, nil
}

// ParseMoney interpreta un decimal en texto ("1234.5"), como llega en el wire.
func ParseMoney(s string, currency Currency) (Money, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Money{}, ErrInvalidAmount
	}
	return NewMoney(d, currency)
}

// Zero devuelve cero en la moneda dada.
func Zero(currency Currency) (Money, error) {
	return NewMoney(decimal.Zero, currency)
}

func checkRange(d decimal.Decimal) error {
	if d.Abs().Cmp(decimal.New(1, moneyMaxIntegerDigs)) >= 0 {
		return ErrMoneyOverflow
	}
	return nil
}

// Amount devuelve el monto como decimal.
func (m Money) Amount() decimal.Decimal { return m.amount }

// Currency devuelve la moneda.
func (m Money) Currency() Currency { return m.currency }

// IsZero indica si el monto es cero.
func (m Money) IsZero() bool { return m.amount.IsZero() }

// IsNegative indica si el monto es menor que cero (ej. reversión de comisión).
func (m Money) IsNegative() bool { return m.amount.IsNegative() }

// Equal compara monto y moneda (100 y 100.00 son iguales).
func (m Money) Equal(o Money) bool {
	return m.currency == o.currency && m.amount.Equal(o.amount)
}

// Cmp compara dos montos de la misma moneda: -1, 0 o 1.
func (m Money) Cmp(o Money) (int, error) {
	if m.currency != o.currency {
		return 0, ErrCurrencyMismatch
	}
	return m.amount.Cmp(o.amount), nil
}

// Add suma dos montos de la misma moneda.
func (m Money) Add(o Money) (Money, error) {
	if m.currency != o.currency {
		return Money{}, ErrCurrencyMismatch
	}
	return m.with(m.amount.Add(o.amount))
}

// Sub resta dos montos de la misma moneda.
func (m Money) Sub(o Money) (Money, error) {
	if m.currency != o.currency {
		return Money{}, ErrCurrencyMismatch
	}
	return m.with(m.amount.Sub(o.amount))
}

// Mul multiplica por un factor (ej. porcentaje de comisión 0.15) y redondea a 4 decimales con
// redondeo bancario (RoundBank), la escala de almacenamiento.
func (m Money) Mul(factor decimal.Decimal) (Money, error) {
	return m.with(m.amount.Mul(factor).RoundBank(MoneyMaxScale))
}

// RoundToMinorUnit redondea a la unidad menor de la moneda (2 decimales) con RoundBank.
func (m Money) RoundToMinorUnit() Money {
	return Money{amount: m.amount.RoundBank(m.currency.minorUnitsOrZero()), currency: m.currency}
}

// Allocate reparte el monto en n partes iguales truncadas a la unidad menor de la moneda; el
// residuo se asigna a la ÚLTIMA parte (DOMAIN.md, PaymentPlan). Σ partes = monto exacto.
// Ej.: 100000 / 3 → 33333.33, 33333.33, 33333.34.
func (m Money) Allocate(n int) ([]Money, error) {
	if n < 1 {
		return nil, ErrInvalidAllocation
	}
	places, ok := m.currency.MinorUnits()
	if !ok {
		return nil, ErrUnsupportedCurrency
	}
	// amount = part·n + residue  ⇒  última parte = part + residue.
	part, residue := m.amount.QuoRem(decimal.NewFromInt(int64(n)), places)
	parts := make([]Money, n)
	for i := range n - 1 {
		parts[i] = Money{amount: part, currency: m.currency}
	}
	parts[n-1] = Money{amount: part.Add(residue), currency: m.currency}
	return parts, nil
}

func (m Money) with(amount decimal.Decimal) (Money, error) {
	if _, ok := m.currency.MinorUnits(); !ok {
		return Money{}, ErrUnsupportedCurrency
	}
	if err := checkRange(amount); err != nil {
		return Money{}, err
	}
	return Money{amount: amount, currency: m.currency}, nil
}

// String devuelve "COP 1234.50" (monto con al menos los decimales de la moneda). Para depuración
// y pruebas; la UI formatea por su cuenta.
func (m Money) String() string {
	places := m.currency.minorUnitsOrZero()
	s := m.amount.String()
	if m.amount.Exponent() > -places {
		s = m.amount.StringFixed(places)
	}
	return string(m.currency) + " " + s
}

// LogValue evita que montos (dato Confidencial, rules/security.md) lleguen a los logs.
func (m Money) LogValue() slog.Value {
	return slog.StringValue(string(m.currency) + " [REDACTED]")
}
