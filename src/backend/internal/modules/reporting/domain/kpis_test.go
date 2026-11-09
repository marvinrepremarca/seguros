package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func TestValidateRange(t *testing.T) {
	t.Parallel()
	d := kernel.DateOf(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	require.NoError(t, ValidateRange(d, d))
	require.NoError(t, ValidateRange(d, d.AddDays(MaxRangeDays)))
	require.ErrorIs(t, ValidateRange(d, d.AddDays(-1)), ErrInvalidRange)
	require.ErrorIs(t, ValidateRange(d, d.AddDays(MaxRangeDays+1)), ErrInvalidRange)
}
