package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender(t *testing.T) {
	t.Parallel()
	for _, tpl := range []string{TplPolicyIssued, TplDueSoon, TplDueToday, TplOverdue, TplArrears, TplReinstated, TplExpiring, TplClaimReported} {
		e, ok := Render(tpl, "Ana", "P-1", Params{DueDate: "2026-12-01", DaysLeft: 30})
		require.True(t, ok, tpl)
		assert.NotEmpty(t, e.Subject)
		assert.Contains(t, e.Body, "Ana")
		assert.Contains(t, e.Body, "P-1")
	}
	e, ok := Render(TplClaimStatus, "Ana", "", Params{Status: "objected"})
	require.True(t, ok)
	assert.Contains(t, e.Body, "objetado")
	assert.Contains(t, e.Body, "su póliza")
	_, ok = Render(TplClaimStatus, "Ana", "", Params{Status: "inventado"})
	assert.False(t, ok)
	_, ok = Render("inventada", "Ana", "", Params{})
	assert.False(t, ok)
}

func TestRenderDropsControlCharacters(t *testing.T) {
	t.Parallel()
	e, ok := Render(TplPolicyIssued, "Ana\n\nPague aquí", "", Params{})
	require.True(t, ok)
	assert.Contains(t, e.Body, "Hola, AnaPague aquí.")
}

func TestBackoff(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 5*time.Minute, Backoff(1))
	assert.Equal(t, 40*time.Minute, Backoff(4))
}

func TestAfterFailure(t *testing.T) {
	t.Parallel()
	s, wait := AfterFailure(1)
	assert.Equal(t, StatusPending, s)
	assert.Equal(t, 5*time.Minute, wait)
	s, _ = AfterFailure(MaxAttempts)
	assert.Equal(t, StatusFailed, s)
}
