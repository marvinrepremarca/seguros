package domain_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/catalog/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func date(t *testing.T, s string) kernel.Date {
	t.Helper()
	d, err := kernel.ParseDate(s)
	require.NoError(t, err)
	return d
}

func TestItem(t *testing.T) {
	t.Parallel()
	i, err := domain.NewItem(uuid.New(), "  Sura ")
	require.NoError(t, err)
	assert.Equal(t, "Sura", i.Name)
	assert.True(t, i.Active)

	_, err = domain.NewItem(uuid.New(), "x")
	require.ErrorIs(t, err, domain.ErrInvalidName)
	_, err = domain.NewItem(uuid.New(), strings.Repeat("x", 121))
	require.ErrorIs(t, err, domain.ErrInvalidName)

	c, err := i.Changed("Sura S.A.", false, 1)
	require.NoError(t, err)
	assert.Equal(t, "Sura S.A.", c.Name)
	assert.False(t, c.Active)
	assert.True(t, i.Active, "inmutable")

	_, err = i.Changed("Sura", true, 2)
	require.ErrorIs(t, err, domain.ErrConcurrentUpdate)
	_, err = i.Changed("x", true, 1)
	require.ErrorIs(t, err, domain.ErrInvalidName)
}

func TestCommissionAgreement(t *testing.T) {
	t.Parallel()
	from, to := date(t, "2026-01-01"), date(t, "2026-12-31")
	pct := decimal.RequireFromString("12.5")
	a, err := domain.NewCommissionAgreement(uuid.New(), uuid.New(), uuid.New(), uuid.NullUUID{}, "", pct, from, to)
	require.NoError(t, err)
	assert.Equal(t, domain.BasisPaidPremium, a.Basis, "base por defecto (N3)")

	cases := []struct {
		name  string
		basis domain.Basis
		pct   string
		from  kernel.Date
		to    kernel.Date
		err   error
	}{
		{"negativo", domain.BasisPaidPremium, "-1", from, to, domain.ErrInvalidPercentage},
		{"mayor a 100", domain.BasisPaidPremium, "100.000001", from, to, domain.ErrInvalidPercentage},
		{"7 decimales", domain.BasisPaidPremium, "1.1234567", from, to, domain.ErrInvalidPercentage},
		{"base inválida", domain.Basis("x"), "1", from, to, domain.ErrInvalidBasis},
		{"vigencia invertida", domain.BasisWrittenPremium, "1", to, from, kernel.ErrInvalidDateRange},
		{"100 es válido", domain.BasisWrittenPremium, "100", from, to, nil},
		{"0 es válido", domain.BasisWrittenPremium, "0", from, to, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewCommissionAgreement(uuid.New(), uuid.New(), uuid.New(), uuid.NullUUID{}, c.basis, decimal.RequireFromString(c.pct), c.from, c.to)
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
				return
			}
			require.NoError(t, err)
		})
	}

	changed, err := a.Changed(domain.BasisWrittenPremium, decimal.RequireFromString("10"), from, to, 1)
	require.NoError(t, err)
	assert.Equal(t, domain.BasisWrittenPremium, changed.Basis)
	assert.Equal(t, domain.BasisPaidPremium, a.Basis, "inmutable")
	_, err = a.Changed(domain.BasisWrittenPremium, pct, from, to, 9)
	require.ErrorIs(t, err, domain.ErrConcurrentUpdate)
}
