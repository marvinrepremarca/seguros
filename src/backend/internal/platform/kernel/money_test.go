package kernel_test

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func money(t *testing.T, s string, c kernel.Currency) kernel.Money {
	t.Helper()
	m, err := kernel.ParseMoney(s, c)
	require.NoError(t, err)
	return m
}

func amounts(ms []kernel.Money) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Amount().String()
	}
	return out
}

func TestNewMoney(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		amount   string
		currency kernel.Currency
		wantErr  error
	}{
		{"COP entero", "100000", kernel.COP, nil},
		{"cuatro decimales", "1234.5678", kernel.COP, nil},
		{"negativo (reversiones)", "-10.5", kernel.USD, nil},
		{"máximo numeric(19,4)", "999999999999999.9999", kernel.COP, nil},
		{"cinco decimales", "1.23456", kernel.COP, kernel.ErrMoneyScale},
		{"desborde", "1000000000000000", kernel.COP, kernel.ErrMoneyOverflow},
		{"desborde negativo", "-1000000000000000", kernel.COP, kernel.ErrMoneyOverflow},
		{"moneda no soportada", "1", kernel.Currency("XYZ"), kernel.ErrUnsupportedCurrency},
		{"moneda vacía", "1", kernel.Currency(""), kernel.ErrUnsupportedCurrency},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, err := kernel.NewMoney(decimal.RequireFromString(tc.amount), tc.currency)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.True(t, m.Amount().Equal(decimal.RequireFromString(tc.amount)))
			assert.Equal(t, tc.currency, m.Currency())
		})
	}
}

func TestParseMoney(t *testing.T) {
	t.Parallel()
	_, err := kernel.ParseMoney("12,5", kernel.COP)
	require.ErrorIs(t, err, kernel.ErrInvalidAmount)
	_, err = kernel.ParseMoney("1e3", kernel.COP)
	require.NoError(t, err)
}

func TestZero(t *testing.T) {
	t.Parallel()
	z, err := kernel.Zero(kernel.COP)
	require.NoError(t, err)
	assert.True(t, z.IsZero())
	assert.False(t, z.IsNegative())
	_, err = kernel.Zero("")
	require.ErrorIs(t, err, kernel.ErrUnsupportedCurrency)
}

func TestMoneyArithmetic(t *testing.T) {
	t.Parallel()
	a := money(t, "100.10", kernel.COP)
	b := money(t, "0.0001", kernel.COP)
	usd := money(t, "1", kernel.USD)

	sum, err := a.Add(b)
	require.NoError(t, err)
	assert.Equal(t, "100.1001", sum.Amount().String())

	diff, err := b.Sub(a)
	require.NoError(t, err)
	assert.Equal(t, "-100.0999", diff.Amount().String())
	assert.True(t, diff.IsNegative())

	_, err = a.Add(usd)
	require.ErrorIs(t, err, kernel.ErrCurrencyMismatch)
	_, err = a.Sub(usd)
	require.ErrorIs(t, err, kernel.ErrCurrencyMismatch)
	_, err = a.Cmp(usd)
	require.ErrorIs(t, err, kernel.ErrCurrencyMismatch)

	maxM := money(t, "999999999999999.9999", kernel.COP)
	_, err = maxM.Add(b)
	require.ErrorIs(t, err, kernel.ErrMoneyOverflow)

	c, err := a.Cmp(b)
	require.NoError(t, err)
	assert.Equal(t, 1, c)
}

func TestMoneyMulRoundsBankToFourDecimals(t *testing.T) {
	t.Parallel()
	tests := []struct{ amount, factor, want string }{
		{"100000", "0.15", "15000"},
		{"1", "0.00005", "0"},      // 0.00005 → par más cercano: 0.0000
		{"1", "0.00015", "0.0002"}, // 0.00015 → 0.0002
		{"1", "0.00025", "0.0002"}, // 0.00025 → 0.0002
		{"33333.3333", "3", "99999.9999"},
		{"-1", "0.00015", "-0.0002"},
	}
	for _, tc := range tests {
		t.Run(tc.amount+"x"+tc.factor, func(t *testing.T) {
			t.Parallel()
			got, err := money(t, tc.amount, kernel.COP).Mul(decimal.RequireFromString(tc.factor))
			require.NoError(t, err)
			assert.True(t, got.Amount().Equal(decimal.RequireFromString(tc.want)), "got %s", got.Amount())
		})
	}
	_, err := money(t, "999999999999999", kernel.COP).Mul(decimal.NewFromInt(2))
	require.ErrorIs(t, err, kernel.ErrMoneyOverflow)
}

func TestMoneyRoundToMinorUnit(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "10.12", money(t, "10.125", kernel.COP).RoundToMinorUnit().Amount().String())
	assert.Equal(t, "10.14", money(t, "10.135", kernel.COP).RoundToMinorUnit().Amount().String())
}

func TestMoneyAllocate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		amount string
		n      int
		want   []string
	}{
		{"100.000 / 3", "100000", 3, []string{"33333.33", "33333.33", "33333.34"}},
		{"divisible exacto", "120000", 12, []string{"10000", "10000", "10000", "10000", "10000", "10000", "10000", "10000", "10000", "10000", "10000", "10000"}},
		{"una parte", "99.99", 1, []string{"99.99"}},
		{"cuatro decimales", "100.0001", 3, []string{"33.33", "33.33", "33.3401"}},
		{"menor que una unidad por parte", "0.01", 3, []string{"0", "0", "0.01"}},
		{"cero", "0", 4, []string{"0", "0", "0", "0"}},
		{"negativo", "-100", 3, []string{"-33.33", "-33.33", "-33.34"}},
		{"residuo grande", "1000000", 7, []string{"142857.14", "142857.14", "142857.14", "142857.14", "142857.14", "142857.14", "142857.16"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := money(t, tc.amount, kernel.COP)
			parts, err := m.Allocate(tc.n)
			require.NoError(t, err)
			require.Len(t, parts, tc.n)
			for i, p := range parts {
				assert.True(t, p.Amount().Equal(decimal.RequireFromString(tc.want[i])), "parte %d: got %s want %s (%v)", i, p.Amount(), tc.want[i], amounts(parts))
				assert.Equal(t, kernel.COP, p.Currency())
			}
			total, err := kernel.Zero(kernel.COP)
			require.NoError(t, err)
			for _, p := range parts {
				total, err = total.Add(p)
				require.NoError(t, err)
			}
			assert.True(t, total.Equal(m), "Σ partes %s ≠ %s", total, m)
		})
	}
}

func TestMoneyAllocateInvalid(t *testing.T) {
	t.Parallel()
	m := money(t, "100", kernel.COP)
	for _, n := range []int{0, -1} {
		_, err := m.Allocate(n)
		require.ErrorIs(t, err, kernel.ErrInvalidAllocation)
	}
	var zero kernel.Money
	_, err := zero.Allocate(2)
	require.ErrorIs(t, err, kernel.ErrUnsupportedCurrency)
	_, err = zero.Mul(decimal.NewFromInt(2))
	require.ErrorIs(t, err, kernel.ErrUnsupportedCurrency)
}

func TestMoneyEqualAndString(t *testing.T) {
	t.Parallel()
	assert.True(t, money(t, "100", kernel.COP).Equal(money(t, "100.00", kernel.COP)))
	assert.False(t, money(t, "100", kernel.COP).Equal(money(t, "100", kernel.USD)))
	assert.Equal(t, "COP 1234.50", money(t, "1234.5", kernel.COP).String())
	assert.Equal(t, "COP 1234.5678", money(t, "1234.5678", kernel.COP).String())
	assert.Equal(t, "USD 0.00", money(t, "0", kernel.USD).String())
	var zero kernel.Money
	assert.Equal(t, " 0", zero.String()) // el valor cero no es un Money válido, pero no falla
}

func TestMoneyLogValueRedacts(t *testing.T) {
	t.Parallel()
	v := money(t, "1234.5", kernel.COP).LogValue()
	assert.Equal(t, slog.KindString, v.Kind())
	assert.Equal(t, "COP [REDACTED]", v.String())
	assert.NotContains(t, v.String(), "1234")
}

func TestMoneyErrorsAreDomainErrors(t *testing.T) {
	t.Parallel()
	_, err := kernel.NewMoney(decimal.NewFromInt(1), "XYZ")
	de, ok := kernel.AsDomainError(err)
	require.True(t, ok)
	assert.Equal(t, "kernel.money.unsupported_currency", de.Code())
	assert.Equal(t, kernel.KindValidation, de.Kind())
	assert.Contains(t, err.Error(), "XYZ")
	assert.True(t, errors.Is(err, kernel.ErrUnsupportedCurrency))
}
