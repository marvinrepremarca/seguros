// Package app calcula los indicadores de la agencia.
package app

import (
	"context"
	"maps"
	"slices"

	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/reporting/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Amount es un valor por moneda.
type Amount struct {
	Currency string
	Value    decimal.Decimal
}

// Bucket es la cartera vencida de un tramo de edad.
type Bucket struct {
	Name, Currency string
	Balance        decimal.Decimal
}

// Counts es una razón entre dos conteos.
type Counts struct{ Total, Part int32 }

// Repository lee las vistas de reporting en el tenant del contexto.
type Repository interface {
	Premiums(ctx context.Context, from, to kernel.Date) ([]Amount, error)
	IssuanceDays(ctx context.Context, from, to kernel.Date) (decimal.Decimal, error)
	PoliciesPerClient(ctx context.Context) (policies, clients int32, err error)
	Conversion(ctx context.Context, from, to kernel.Date) (Counts, error)
	Retention(ctx context.Context, from, to kernel.Date) (Counts, error)
	Overdue(ctx context.Context, today kernel.Date) ([]Bucket, error)
	Accrued(ctx context.Context, from, to kernel.Date) ([]Amount, error)
	Received(ctx context.Context, from, to kernel.Date) ([]Amount, error)
	ClaimDays(ctx context.Context, from, to kernel.Date) (decimal.Decimal, error)
	PaidClaims(ctx context.Context, from, to kernel.Date) ([]Amount, error)
}

// Store abre una transacción del tenant.
type Store interface {
	WithinTenant(ctx context.Context, fn func(ctx context.Context, r Repository) error) error
}

// Service calcula los indicadores.
type Service struct {
	Store   Store
	Checker flags.Checker
	Clock   kernel.Clock
}

// CommissionLine compara lo causado con lo recibido.
type CommissionLine struct {
	Currency          string
	Accrued, Received decimal.Decimal
}

// LossRatio es la siniestralidad de una moneda.
type LossRatio struct {
	Currency      string
	Paid, Premium decimal.Decimal
	Percent       decimal.Decimal
	HasPercent    bool
}

// Kpis son los indicadores; lo opcional es nil si su módulo está apagado para el tenant.
type Kpis struct {
	Premiums          []Amount
	IssuanceDays      decimal.Decimal
	Policies, Clients int32
	Conversion        *Counts
	Retention         *Counts
	Overdue           []Bucket
	HasOverdue        bool
	Commissions       []CommissionLine
	HasCommissions    bool
	ClaimDays         *decimal.Decimal
	LossRatio         []LossRatio
}

func commissionLines(accrued, received []Amount) []CommissionLine {
	byCurrency := map[string]*CommissionLine{}
	out := []CommissionLine{}
	line := func(c string) *CommissionLine {
		if l, ok := byCurrency[c]; ok {
			return l
		}
		l := &CommissionLine{Currency: c}
		byCurrency[c] = l
		return l
	}
	for _, a := range accrued {
		line(a.Currency).Accrued = a.Value
	}
	for _, a := range received {
		line(a.Currency).Received = a.Value
	}
	for _, c := range sortedKeys(byCurrency) {
		out = append(out, *byCurrency[c])
	}
	return out
}

func lossRatios(paid, premiums []Amount) []LossRatio {
	byCurrency := map[string]*LossRatio{}
	line := func(c string) *LossRatio {
		if l, ok := byCurrency[c]; ok {
			return l
		}
		l := &LossRatio{Currency: c}
		byCurrency[c] = l
		return l
	}
	for _, a := range paid {
		line(a.Currency).Paid = a.Value
	}
	for _, a := range premiums {
		line(a.Currency).Premium = a.Value
	}
	out := []LossRatio{}
	for _, c := range sortedKeys(byCurrency) {
		l := byCurrency[c]
		l.Percent, l.HasPercent = lossRatioPercent(l.Paid, l.Premium)
		out = append(out, *l)
	}
	return out
}

// Get calcula los indicadores del periodo, ambos extremos inclusive.
func (s *Service) Get(ctx context.Context, from, to kernel.Date) (Kpis, error) {
	if err := authz.Require(ctx, authz.ReportingRead); err != nil {
		return Kpis{}, err
	}
	if err := domain.ValidateRange(from, to); err != nil {
		return Kpis{}, err
	}
	on := func(k flags.Key) bool { return s.Checker.IsEnabled(ctx, k) }
	var out Kpis
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, r Repository) error {
		var err error
		if out.Premiums, err = r.Premiums(ctx, from, to); err != nil {
			return err
		}
		if out.IssuanceDays, err = r.IssuanceDays(ctx, from, to); err != nil {
			return err
		}
		if out.Policies, out.Clients, err = r.PoliciesPerClient(ctx); err != nil {
			return err
		}
		if on(flags.QuotingModule) {
			c, err := r.Conversion(ctx, from, to)
			if err != nil {
				return err
			}
			out.Conversion = &c
		}
		if on(flags.RenewalsModule) {
			c, err := r.Retention(ctx, from, to)
			if err != nil {
				return err
			}
			out.Retention = &c
		}
		if on(flags.BillingModule) {
			if out.Overdue, err = r.Overdue(ctx, kernel.Today(s.Clock)); err != nil {
				return err
			}
			out.HasOverdue = true
		}
		if on(flags.CommissionsModule) {
			accrued, err := r.Accrued(ctx, from, to)
			if err != nil {
				return err
			}
			received, err := r.Received(ctx, from, to)
			if err != nil {
				return err
			}
			out.Commissions, out.HasCommissions = commissionLines(accrued, received), true
		}
		if on(flags.ClaimsModule) {
			days, err := r.ClaimDays(ctx, from, to)
			if err != nil {
				return err
			}
			out.ClaimDays = &days
			paid, err := r.PaidClaims(ctx, from, to)
			if err != nil {
				return err
			}
			out.LossRatio = lossRatios(paid, out.Premiums)
		}
		return nil
	})
	return out, err
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// lossRatioPercent es lo pagado en siniestros sobre las primas emitidas, en porcentaje con dos
// decimales; ok es falso si no hay primas.
func lossRatioPercent(paid, premium decimal.Decimal) (decimal.Decimal, bool) {
	if !premium.IsPositive() {
		return decimal.Zero, false
	}
	return paid.Mul(decimal.NewFromInt(100)).DivRound(premium, 2), true
}
