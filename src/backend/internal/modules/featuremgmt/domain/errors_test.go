package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/domain"
)

func TestNewReason(t *testing.T) {
	t.Parallel()
	r, err := domain.NewReason("  Add-on contratado  ")
	require.NoError(t, err)
	assert.Equal(t, domain.Reason("Add-on contratado"), r)
	for _, bad := range []string{"", "  ", "ab", strings.Repeat("x", 501)} {
		_, err := domain.NewReason(bad)
		require.ErrorIs(t, err, domain.ErrInvalidReason, "%q", bad)
	}
	_, err = domain.NewReason(strings.Repeat("ñ", 500))
	require.NoError(t, err, "cuenta caracteres, no bytes")
}
