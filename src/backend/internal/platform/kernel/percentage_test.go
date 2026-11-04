package kernel_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func TestNewPercentage(t *testing.T) {
	t.Parallel()
	for _, valid := range []string{"0", "100", "12.5", "0.000001", "99.999999"} {
		p, err := kernel.NewPercentage(decimal.RequireFromString(valid))
		require.NoError(t, err, valid)
		assert.Equal(t, valid, p.String())
	}
	for _, invalid := range []string{"-0.000001", "100.000001", "1.1234567"} {
		_, err := kernel.NewPercentage(decimal.RequireFromString(invalid))
		require.ErrorIs(t, err, kernel.ErrInvalidPercentage, invalid)
	}
}
