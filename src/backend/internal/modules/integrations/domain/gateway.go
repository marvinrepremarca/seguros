// Package domain contiene el lenguaje propio de la capa anticorrupción con aseguradoras.
package domain

import (
	"strings"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de negocio.
var (
	ErrNotFound    = kernel.NewError(kernel.KindNotFound, "integrations.connector.not_found", "conector inexistente")
	ErrDisabled    = kernel.NewError(kernel.KindUnavailable, "integrations.connector.disabled", "el conector está deshabilitado")
	ErrUnavailable = kernel.NewError(kernel.KindUnavailable, "integrations.connector.unavailable", "la aseguradora no respondió")
	ErrInvalid     = kernel.NewError(kernel.KindValidation, "integrations.quote.invalid", "los datos de la consulta no son válidos")
)

// QuoteQuery es lo que se le pregunta a una aseguradora. No lleva datos personales.
type QuoteQuery struct {
	LineOfBusiness string
	SumInsured     kernel.Money
}

// NewQuoteQuery valida la consulta.
func NewQuoteQuery(line string, sum kernel.Money) (QuoteQuery, error) {
	line = strings.TrimSpace(line)
	if len(line) < 2 || !sum.Amount().IsPositive() {
		return QuoteQuery{}, ErrInvalid
	}
	return QuoteQuery{LineOfBusiness: line, SumInsured: sum}, nil
}

// QuoteResult es la respuesta traducida al lenguaje del ERP.
type QuoteResult struct {
	Reference  string
	NetPremium kernel.Money
	Simulated  bool
}
