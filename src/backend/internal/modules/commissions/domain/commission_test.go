package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/commissions/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func cop(t *testing.T, s string) kernel.Money {
	t.Helper()
	m, err := kernel.ParseMoney(s, "COP")
	require.NoError(t, err)
	return m
}

func pct(t *testing.T, s string) kernel.Percentage {
	t.Helper()
	p, err := kernel.NewPercentage(decimal.RequireFromString(s))
	require.NoError(t, err)
	return p
}

func TestShare(t *testing.T) {
	t.Parallel()
	m, err := domain.Share(cop(t, "100000"), pct(t, "12.5"))
	require.NoError(t, err)
	assert.Equal(t, "12500", m.Amount().String())
	m, err = domain.Share(cop(t, "-1000"), pct(t, "10"))
	require.NoError(t, err)
	assert.Equal(t, "-100", m.Amount().String(), "un reverso o clawback conserva el signo")
}

func TestNetShareOfPayment(t *testing.T) {
	t.Parallel()
	m, err := domain.NetShareOfPayment(cop(t, "119"), cop(t, "1000"), cop(t, "190"))
	require.NoError(t, err)
	assert.Equal(t, "100", m.Amount().String())
	m, err = domain.NetShareOfPayment(cop(t, "50"), cop(t, "0"), cop(t, "0"))
	require.NoError(t, err)
	assert.True(t, m.IsZero())
}
