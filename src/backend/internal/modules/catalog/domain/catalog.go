// Package domain contiene el catálogo de la agencia: aseguradoras, ramos, productos y acuerdos de
// comisión.
package domain

import (
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de dominio de catalog (códigos estables).
var (
	ErrInvalidName        = kernel.NewError(kernel.KindValidation, "catalog.item.invalid_name", "nombre de 2 a 120 caracteres")
	ErrNameTaken          = kernel.NewError(kernel.KindConflict, "catalog.item.name_taken", "ya existe un registro con ese nombre")
	ErrNotFound           = kernel.NewError(kernel.KindNotFound, "catalog.item.not_found", "registro inexistente")
	ErrConcurrentUpdate   = kernel.NewError(kernel.KindConflict, "catalog.item.concurrent_update", "el registro cambió; recargue y reintente")
	ErrInvalidRiskSchema  = kernel.NewError(kernel.KindValidation, "catalog.line_of_business.invalid_risk_schema", "esquema de riesgo desconocido")
	ErrInvalidBasis       = kernel.NewError(kernel.KindValidation, "catalog.commission_agreement.invalid_basis", "base de comisión desconocida")
	ErrInvalidPercentage  = kernel.NewError(kernel.KindValidation, "catalog.commission_agreement.invalid_percentage", "porcentaje entre 0 y 100, con hasta 6 decimales")
	ErrAgreementOverlap   = kernel.NewError(kernel.KindConflict, "catalog.commission_agreement.overlap", "ya hay un acuerdo vigente para ese ámbito en esas fechas")
	ErrAgreementReference = kernel.NewError(kernel.KindValidation, "catalog.commission_agreement.invalid_reference", "aseguradora, ramo o producto inexistente o incoherente")
)

// Item es un registro con nombre y estado activo (aseguradora, ramo o producto). Inmutable: los
// cambios devuelven una copia.
type Item struct {
	ID      uuid.UUID
	Name    string
	Active  bool
	Version int32
}

// NewItem crea un registro activo.
func NewItem(id uuid.UUID, name string) (Item, error) {
	n, err := validName(name)
	if err != nil {
		return Item{}, err
	}
	return Item{ID: id, Name: n, Active: true, Version: 1}, nil
}

// Changed aplica nombre y estado sobre la versión esperada.
func (i Item) Changed(name string, active bool, expectedVersion int32) (Item, error) {
	if i.Version != expectedVersion {
		return Item{}, ErrConcurrentUpdate
	}
	n, err := validName(name)
	if err != nil {
		return Item{}, err
	}
	i.Name, i.Active = n, active
	return i, nil
}

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 2 || n > 120 {
		return "", ErrInvalidName
	}
	return name, nil
}

// LineOfBusiness es un ramo con su esquema de datos de riesgo (no cambia: los bienes asegurados ya
// guardados dependen de él).
type LineOfBusiness struct {
	Item
	RiskSchemaKey string
}

// InsuranceProduct es un producto de una aseguradora en un ramo.
type InsuranceProduct struct {
	Item
	InsurerID        uuid.UUID
	LineOfBusinessID uuid.UUID
}

// Basis es la base sobre la que se causa la comisión (DECISIONS N3).
type Basis string

// Bases de comisión.
const (
	BasisPaidPremium    Basis = "paid_premium"
	BasisWrittenPremium Basis = "written_premium"
)

//nolint:gochecknoglobals // constante decimal
var hundred = decimal.NewFromInt(100)

// CommissionAgreement es la comisión pactada con una aseguradora para un ramo y, opcionalmente, un
// producto, durante una vigencia.
type CommissionAgreement struct {
	ID               uuid.UUID
	InsurerID        uuid.UUID
	LineOfBusinessID uuid.UUID
	ProductID        uuid.NullUUID
	Basis            Basis
	Percentage       decimal.Decimal
	Validity         kernel.DateRange
	Version          int32
}

// NewCommissionAgreement valida el acuerdo. Sin base usa prima pagada (DECISIONS N3).
func NewCommissionAgreement(id, insurer, line uuid.UUID, product uuid.NullUUID, basis Basis, pct decimal.Decimal, from, to kernel.Date) (CommissionAgreement, error) {
	a := CommissionAgreement{ID: id, InsurerID: insurer, LineOfBusinessID: line, ProductID: product, Version: 1}
	if basis == "" {
		basis = BasisPaidPremium
	}
	if err := a.set(basis, pct, from, to); err != nil {
		return CommissionAgreement{}, err
	}
	return a, nil
}

// Changed aplica los términos sobre la versión esperada.
func (a CommissionAgreement) Changed(basis Basis, pct decimal.Decimal, from, to kernel.Date, expectedVersion int32) (CommissionAgreement, error) {
	if a.Version != expectedVersion {
		return CommissionAgreement{}, ErrConcurrentUpdate
	}
	if err := a.set(basis, pct, from, to); err != nil {
		return CommissionAgreement{}, err
	}
	return a, nil
}

func (a *CommissionAgreement) set(basis Basis, pct decimal.Decimal, from, to kernel.Date) error {
	if basis != BasisPaidPremium && basis != BasisWrittenPremium {
		return ErrInvalidBasis
	}
	if pct.IsNegative() || pct.GreaterThan(hundred) || !pct.Equal(pct.Truncate(6)) {
		return ErrInvalidPercentage
	}
	validity, err := kernel.NewDateRange(from, to)
	if err != nil {
		return err
	}
	a.Basis, a.Percentage, a.Validity = basis, pct, validity
	return nil
}
