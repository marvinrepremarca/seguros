// Package simulator es una aseguradora de mentira para probar la capa sin un portal real. Sus valores
// no son una oferta: el resultado va marcado Simulated y solo se registra fuera de producción.
package simulator

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/integrations/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Gateway implementa app.Gateway.
type Gateway struct{}

// Quote devuelve una prima ficticia del 5 % de la suma asegurada.
func (Gateway) Quote(_ context.Context, q domain.QuoteQuery) (domain.QuoteResult, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return domain.QuoteResult{}, fmt.Errorf("simulador: referencia: %w", err)
	}
	premium, err := kernel.NewMoney(q.SumInsured.Amount().Mul(decimal.RequireFromString("0.05")).RoundBank(2), q.SumInsured.Currency())
	if err != nil {
		return domain.QuoteResult{}, fmt.Errorf("simulador: prima: %w", err)
	}
	return domain.QuoteResult{Reference: "SIM-" + id.String(), NetPremium: premium, Simulated: true}, nil
}
