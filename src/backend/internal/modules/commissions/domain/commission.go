// Package domain contiene los cálculos de comisión (Flujo 9, DECISIONS N3, N5, N14).
package domain

import "github.com/marvinrepremarca/seguros/internal/platform/kernel"

// Kind es el origen de una comisión causada.
type Kind string

// Orígenes.
const (
	KindPayment         Kind = "payment"
	KindPaymentReversal Kind = "payment_reversal"
	KindWritten         Kind = "written"
	KindEndorsement     Kind = "endorsement"
	KindClawback        Kind = "clawback"
)

// Bases del acuerdo (coinciden con catalog).
const (
	BasisPaid    = "paid_premium"
	BasisWritten = "written_premium"
)

// ErrInvalidTolerance indica una tolerancia negativa.
var ErrInvalidTolerance = kernel.NewError(kernel.KindValidation, "commissions.settings.invalid_tolerance", "la tolerancia debe estar entre 0 y COP 400.000")

// defaultToleranceCOP y maxToleranceCOP son la tolerancia por póliza y su máximo (DECISIONS T4, N26).
const (
	defaultToleranceCOP = "1000"
	maxToleranceCOP     = "400000"
	maxToleranceOther   = "100" // referencia de mercado en USD (N26)
)

// DefaultTolerance devuelve la tolerancia cuando la agencia no ha fijado una: en COP la de T4; en otra
// moneda, cero, es decir, coincidencia exacta (N26).
func DefaultTolerance(currency kernel.Currency) (kernel.Money, error) {
	if currency == copCurrency {
		return kernel.ParseMoney(defaultToleranceCOP, copCurrency)
	}
	return kernel.Zero(currency)
}

// ValidateTolerance rechaza valores negativos y, en COP, por encima del máximo.
func ValidateTolerance(t kernel.Money) error {
	if t.IsNegative() {
		return ErrInvalidTolerance
	}
	maximum := maxToleranceOther
	if t.Currency() == copCurrency {
		maximum = maxToleranceCOP
	}
	limit, err := kernel.ParseMoney(maximum, t.Currency())
	if err != nil {
		return err
	}
	c, err := t.Cmp(limit)
	if err != nil {
		return err
	}
	if c > 0 {
		return ErrInvalidTolerance
	}
	return nil
}

// Share aplica un porcentaje con redondeo a la unidad menor de la moneda (RoundBank).
func Share(base kernel.Money, pct kernel.Percentage) (kernel.Money, error) {
	m, err := base.Mul(pct.Decimal().Shift(-2))
	if err != nil {
		return kernel.Money{}, err
	}
	return m.RoundToMinorUnit(), nil
}

// NetShareOfPayment devuelve la parte de prima neta de un valor pagado, en proporción a neta/total
// (DECISIONS N14). Sin total, no hay prima que comisionar.
func NetShareOfPayment(paid, net, taxes kernel.Money) (kernel.Money, error) {
	total, err := net.Add(taxes)
	if err != nil {
		return kernel.Money{}, err
	}
	if total.IsZero() {
		return kernel.Zero(paid.Currency())
	}
	m, err := paid.Mul(net.Amount().Div(total.Amount()))
	if err != nil {
		return kernel.Money{}, err
	}
	return m.RoundToMinorUnit(), nil
}
