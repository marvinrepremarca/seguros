// Package app orquesta los conectores con aseguradoras detrás del kill switch de cada uno.
package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/integrations/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

const callTimeout = 15 * time.Second

// Gateway es el puerto que implementa cada aseguradora; su adaptador traduce su API a este lenguaje.
type Gateway interface {
	Quote(ctx context.Context, q domain.QuoteQuery) (domain.QuoteResult, error)
}

// Connector es una aseguradora registrada.
type Connector struct {
	Key, Name string
	Simulated bool
	// KillSwitch apaga el conector para todos los tenants.
	KillSwitch flags.Key
	Gateway    Gateway
}

// CallLog registra cada llamada, sin datos del riesgo.
type CallLog interface {
	Record(ctx context.Context, connector, operation, outcome string, at time.Time) error
}

// Service da acceso a los conectores.
type Service struct {
	connectors []Connector
	checker    flags.Checker
	log        CallLog
	clock      kernel.Clock
}

// NewService construye el servicio.
func NewService(connectors []Connector, checker flags.Checker, log CallLog, clock kernel.Clock) *Service {
	return &Service{connectors: connectors, checker: checker, log: log, clock: clock}
}

// Status es un conector con su estado.
type Status struct {
	Key, Name          string
	Simulated, Enabled bool
}

// List devuelve los conectores registrados.
func (s *Service) List(ctx context.Context) ([]Status, error) {
	if err := authz.Require(ctx, authz.IntegrationRead); err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(s.connectors))
	for _, e := range s.connectors {
		out = append(out, Status{Key: e.Key, Name: e.Name, Simulated: e.Simulated, Enabled: s.checker.IsEnabled(ctx, e.KillSwitch)})
	}
	return out, nil
}

// Quote consulta una cotización. Con el kill switch apagado no se llama a la aseguradora.
func (s *Service) Quote(ctx context.Context, key, line string, sum decimal.Decimal) (domain.QuoteResult, error) {
	if err := authz.Require(ctx, authz.IntegrationQuote); err != nil {
		return domain.QuoteResult{}, err
	}
	var e *Connector
	for i := range s.connectors {
		if s.connectors[i].Key == key {
			e = &s.connectors[i]
		}
	}
	if e == nil {
		return domain.QuoteResult{}, domain.ErrNotFound
	}
	if !s.checker.IsEnabled(ctx, e.KillSwitch) {
		return domain.QuoteResult{}, domain.ErrDisabled
	}
	amount, err := kernel.NewMoney(sum, kernel.COP)
	if err != nil {
		return domain.QuoteResult{}, domain.ErrInvalid
	}
	q, err := domain.NewQuoteQuery(line, amount)
	if err != nil {
		return domain.QuoteResult{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	res, callErr := e.Gateway.Quote(callCtx, q)
	outcome := "success"
	if callErr != nil {
		outcome = "failure"
	}
	if err := s.log.Record(ctx, e.Key, "quote", outcome, s.clock.Now()); err != nil {
		return domain.QuoteResult{}, fmt.Errorf("integrations: bitácora: %w", err)
	}
	if callErr != nil {
		var known *kernel.Error
		if errors.As(callErr, &known) {
			return domain.QuoteResult{}, callErr
		}
		return domain.QuoteResult{}, domain.ErrUnavailable
	}
	return res, nil
}
