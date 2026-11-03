package kernel_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7())
	got, err := kernel.DecodeCursor(kernel.EncodeCursor(id))
	require.NoError(t, err)
	assert.True(t, got.Valid)
	assert.Equal(t, id, got.UUID)

	empty, err := kernel.DecodeCursor("")
	require.NoError(t, err)
	assert.False(t, empty.Valid)

	for _, bad := range []string{"###", "AAAA"} {
		_, err := kernel.DecodeCursor(bad)
		require.ErrorIs(t, err, kernel.ErrInvalidCursor, bad)
	}
}

func TestPageSize(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int32(kernel.DefaultPageSize), kernel.PageSize(0))
	assert.Equal(t, int32(10), kernel.PageSize(10))
	assert.Equal(t, int32(kernel.MaxPageSize), kernel.PageSize(1000))
}
