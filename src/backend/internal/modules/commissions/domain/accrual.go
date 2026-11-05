package domain

import (
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ErrAlreadyReconciled indica que la causación ya pertenece a una liquidación.
var ErrAlreadyReconciled = kernel.NewError(kernel.KindConflict, "commissions.accrual.already_reconciled", "la comisión ya está conciliada")

// copCurrency es la moneda en la que está definida la tolerancia (DECISIONS T4, N26).
const copCurrency kernel.Currency = "COP"

// AccrualInput son los datos de una causación. Sin acuerdo, Percentage es nil: queda en 0 % con alerta.
type AccrualInput struct {
	ID, PolicyID, SourceID uuid.UUID
	Kind                   Kind
	Base                   kernel.Money
	Percentage             *kernel.Percentage
	AgentID                uuid.NullUUID
	On                     kernel.Date
}

// AccrualSnapshot son los datos de una causación para persistir y rehidratar.
type AccrualSnapshot struct {
	ID, PolicyID, SourceID uuid.UUID
	Kind                   Kind
	Base, Amount           kernel.Money
	Percentage             kernel.Percentage
	AgentID                uuid.NullUUID
	AccruedOn              kernel.Date
	MissingAgreement       bool
	StatementID            uuid.NullUUID
}

// Accrual es una comisión causada. Su valor no cambia; una corrección es otra causación.
type Accrual struct{ s AccrualSnapshot }

// NewAccrual calcula la comisión de la base (DECISIONS N3, N14).
func NewAccrual(in AccrualInput, zero kernel.Percentage) (*Accrual, error) {
	pct, missing := zero, true
	if in.Percentage != nil {
		pct, missing = *in.Percentage, false
	}
	amount, err := Share(in.Base, pct)
	if err != nil {
		return nil, err
	}
	return &Accrual{s: AccrualSnapshot{
		ID: in.ID, PolicyID: in.PolicyID, SourceID: in.SourceID, Kind: in.Kind, Base: in.Base, Amount: amount, Percentage: pct,
		AgentID: in.AgentID, AccruedOn: in.On, MissingAgreement: missing,
	}}, nil
}

// RehydrateAccrual reconstruye la causación.
func RehydrateAccrual(s AccrualSnapshot) *Accrual { return &Accrual{s: s} }

// Snapshot devuelve una copia.
func (a *Accrual) Snapshot() AccrualSnapshot { return a.s }

// Reversal deshace esta causación con su mismo asesor, porcentaje y fecha, para que la comisión interna
// use la misma tasa.
func (a *Accrual) Reversal(id uuid.UUID) (*Accrual, error) {
	zero, err := kernel.Zero(a.s.Amount.Currency())
	if err != nil {
		return nil, err
	}
	base, err := zero.Sub(a.s.Base)
	if err != nil {
		return nil, err
	}
	amount, err := zero.Sub(a.s.Amount)
	if err != nil {
		return nil, err
	}
	return &Accrual{s: AccrualSnapshot{
		ID: id, PolicyID: a.s.PolicyID, SourceID: a.s.SourceID, Kind: KindPaymentReversal, Base: base, Amount: amount, Percentage: a.s.Percentage,
		AgentID: a.s.AgentID, AccruedOn: a.s.AccruedOn,
	}}, nil
}

// NewReversalMarker registra un reverso entregado antes que su pago: vale cero e impide causarlo después.
func NewReversalMarker(id, policyID, paymentID uuid.UUID, currency kernel.Currency, zero kernel.Percentage, on kernel.Date) (*Accrual, error) {
	z, err := kernel.Zero(currency)
	if err != nil {
		return nil, err
	}
	return &Accrual{s: AccrualSnapshot{ID: id, PolicyID: policyID, SourceID: paymentID, Kind: KindPaymentReversal, Base: z, Amount: z, Percentage: zero, AccruedOn: on}}, nil
}

// MarkReconciled la vincula a una liquidación: la transición es única.
func (a *Accrual) MarkReconciled(statement uuid.UUID) error {
	if a.s.StatementID.Valid {
		return ErrAlreadyReconciled
	}
	a.s.StatementID = uuid.NullUUID{UUID: statement, Valid: true}
	return nil
}

// Reconcile compara lo liquidado con lo causado sin conciliar. Dentro de la tolerancia, las concilia y
// devuelve true (DECISIONS T4, N26).
func Reconcile(pending []*Accrual, statement uuid.UUID, reported, tolerance kernel.Money) (bool, kernel.Money, error) {
	expected, err := kernel.Zero(reported.Currency())
	if err != nil {
		return false, kernel.Money{}, err
	}
	for _, a := range pending {
		if expected, err = expected.Add(a.s.Amount); err != nil {
			return false, kernel.Money{}, err
		}
	}
	diff, err := expected.Sub(reported)
	if err != nil {
		return false, kernel.Money{}, err
	}
	if diff.IsNegative() {
		if diff, err = reported.Sub(expected); err != nil {
			return false, kernel.Money{}, err
		}
	}
	c, err := diff.Cmp(tolerance)
	if err != nil {
		return false, kernel.Money{}, err
	}
	if c > 0 {
		return false, expected, nil
	}
	for _, a := range pending {
		if err := a.MarkReconciled(statement); err != nil {
			return false, kernel.Money{}, err
		}
	}
	return true, expected, nil
}
