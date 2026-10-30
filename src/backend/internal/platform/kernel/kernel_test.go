package kernel_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func TestSystemClockIsUTC(t *testing.T) {
	t.Parallel()
	now := kernel.SystemClock{}.Now()
	assert.Equal(t, time.UTC, now.Location())
	assert.WithinDuration(t, time.Now(), now, time.Minute)
}

func TestFakeClock(t *testing.T) {
	t.Parallel()
	bogota := time.FixedZone("COT", -5*3600)
	c := kernel.NewFakeClock(time.Date(2026, 10, 30, 7, 0, 0, 0, bogota))
	assert.Equal(t, time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), c.Now())
	assert.Equal(t, time.UTC, c.Now().Location())

	c.Advance(36 * time.Hour)
	assert.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), c.Now())

	c.Set(time.Date(2027, 1, 1, 0, 0, 0, 0, bogota))
	assert.Equal(t, time.Date(2027, 1, 1, 5, 0, 0, 0, time.UTC), c.Now())

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			c.Advance(time.Second)
			_ = c.Now()
		})
	}
	wg.Wait()
	assert.Equal(t, time.Date(2027, 1, 1, 5, 0, 10, 0, time.UTC), c.Now())
}

type policyID uuid.UUID

func TestNewIDIsUUIDv7(t *testing.T) {
	t.Parallel()
	a, err := kernel.NewID[policyID]()
	require.NoError(t, err)
	b, err := kernel.NewID[policyID]()
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
	assert.Equal(t, uuid.Version(7), uuid.UUID(a).Version())
	assert.False(t, kernel.IsZeroID(a))
	assert.True(t, kernel.IsZeroID(policyID{}))
	assert.Less(t, kernel.IDString(a), kernel.IDString(b), "UUIDv7 ordenable por tiempo")
}

func TestParseID(t *testing.T) {
	t.Parallel()
	id, err := kernel.NewID[policyID]()
	require.NoError(t, err)
	got, err := kernel.ParseID[policyID](kernel.IDString(id))
	require.NoError(t, err)
	assert.Equal(t, id, got)

	for _, bad := range []string{
		"",
		"not-a-uuid",
		uuid.NewString(), // v4
		"urn:uuid:" + kernel.IDString(id),
		"{" + kernel.IDString(id) + "}",
	} {
		_, err := kernel.ParseID[policyID](bad)
		require.ErrorIs(t, err, kernel.ErrInvalidID, bad)
	}
}

func TestTenantAndUserIDString(t *testing.T) {
	t.Parallel()
	u := uuid.MustParse("0192d6a0-0000-7000-8000-000000000001")
	assert.Equal(t, u.String(), kernel.TenantID(u).String())
	assert.Equal(t, u.String(), kernel.UserID(u).String())
}

func TestDomainError(t *testing.T) {
	t.Parallel()
	errNotFound := kernel.NewError(kernel.KindNotFound, "policies.policy.not_found", "póliza no encontrada")
	detailed := errNotFound.WithMessage("póliza %s", "abc")
	wrapped := fmt.Errorf("cargando: %w", detailed)

	assert.Equal(t, "policies.policy.not_found: póliza abc", detailed.Error())
	assert.ErrorIs(t, wrapped, errNotFound, "mismo código = mismo error")
	assert.NotErrorIs(t, wrapped, kernel.NewError(kernel.KindNotFound, "otro.codigo", ""))
	assert.False(t, errNotFound.Is(errors.New("x")))

	de, ok := kernel.AsDomainError(wrapped)
	require.True(t, ok)
	assert.Equal(t, "policies.policy.not_found", de.Code())
	assert.Equal(t, kernel.KindNotFound, de.Kind())

	de, ok = kernel.AsDomainError(errors.New("infra"))
	assert.False(t, ok)
	assert.Nil(t, de)
}
