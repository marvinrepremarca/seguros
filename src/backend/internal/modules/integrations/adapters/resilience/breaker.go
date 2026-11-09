// Package resilience envuelve los conectores con un circuit breaker.
package resilience

import (
	"context"

	"github.com/sony/gobreaker/v2"

	"github.com/marvinrepremarca/seguros/internal/modules/integrations/app"
	"github.com/marvinrepremarca/seguros/internal/modules/integrations/domain"
)

type breakerGateway struct {
	next    app.Gateway
	breaker *gobreaker.CircuitBreaker[domain.QuoteResult]
}

// WithBreaker corta las llamadas a una aseguradora que falla de forma repetida.
func WithBreaker(name string, next app.Gateway) app.Gateway {
	return &breakerGateway{next: next, breaker: gobreaker.NewCircuitBreaker[domain.QuoteResult](gobreaker.Settings{Name: name})}
}

// Quote implementa app.Gateway.
func (g *breakerGateway) Quote(ctx context.Context, q domain.QuoteQuery) (domain.QuoteResult, error) {
	return g.breaker.Execute(func() (domain.QuoteResult, error) { return g.next.Quote(ctx, q) })
}
