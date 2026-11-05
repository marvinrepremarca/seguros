package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/renewals/domain"
)

func TestTransitions(t *testing.T) {
	t.Parallel()
	statuses := []domain.Status{domain.StatusOpen, domain.StatusInProgress, domain.StatusRenewed, domain.StatusLost, domain.StatusClosed}
	allowed := map[domain.Status]map[string]bool{
		domain.StatusOpen:       {"start": true, "lost": true, "renewed": true},
		domain.StatusInProgress: {"lost": true, "renewed": true},
		domain.StatusLost:       {"renewed": true},
	}
	ops := map[string]func(*domain.Case) error{
		"start":   func(c *domain.Case) error { return c.Start() },
		"lost":    func(c *domain.Case) error { return c.MarkLost("precio alto") },
		"renewed": func(c *domain.Case) error { return c.MarkRenewed(uuid.New()) },
	}
	for _, from := range statuses {
		for name, op := range ops {
			c := domain.Rehydrate(domain.Snapshot{Status: from})
			err := op(c)
			if allowed[from][name] {
				require.NoError(t, err, "%s %s", from, name)
			} else {
				require.ErrorIs(t, err, domain.ErrInvalidTransition, "%s %s", from, name)
			}
		}
	}
}

func TestLostNeedsReason(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, domain.Rehydrate(domain.Snapshot{Status: domain.StatusOpen}).MarkLost("  "), domain.ErrInvalidReason)
}

func TestCloseKeepsRenewed(t *testing.T) {
	t.Parallel()
	c := domain.Rehydrate(domain.Snapshot{Status: domain.StatusRenewed})
	c.Close()
	assert.Equal(t, domain.StatusRenewed, c.Snapshot().Status)
	o := domain.Rehydrate(domain.Snapshot{Status: domain.StatusOpen})
	o.Close()
	assert.Equal(t, domain.StatusClosed, o.Snapshot().Status)
}

func TestDueAlerts(t *testing.T) {
	t.Parallel()
	d := domain.DefaultAlertDays()
	assert.Empty(t, domain.DueAlerts(d, nil, 61))
	assert.Equal(t, []int32{60}, domain.DueAlerts(d, nil, 60))
	assert.Equal(t, []int32{60, 30}, domain.DueAlerts(d, nil, 20), "el worker estuvo caído")
	assert.Equal(t, []int32{30}, domain.DueAlerts(d, []int32{60}, 30))
}

func TestValidateAlertDays(t *testing.T) {
	t.Parallel()
	require.NoError(t, domain.ValidateAlertDays([]int32{60, 30}))
	for _, bad := range [][]int32{nil, {0}, {366}, {30, 30}} {
		require.ErrorIs(t, domain.ValidateAlertDays(bad), domain.ErrInvalidAlertDays)
	}
}
