package app

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLossRatio(t *testing.T) {
	t.Parallel()
	r, ok := lossRatioPercent(decimal.NewFromInt(1), decimal.NewFromInt(3))
	require.True(t, ok)
	assert.Equal(t, "33.33", r.String())
	_, ok = lossRatioPercent(decimal.NewFromInt(1), decimal.Zero)
	assert.False(t, ok)
}
