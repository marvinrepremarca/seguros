package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/claims/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

var now = time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // constante de prueba

func date(t *testing.T, s string) kernel.Date {
	t.Helper()
	d, err := kernel.ParseDate(s)
	require.NoError(t, err)
	return d
}

func cop(t *testing.T, v int64) kernel.Money {
	t.Helper()
	m, err := kernel.NewMoney(decimal.NewFromInt(v), "COP")
	require.NoError(t, err)
	return m
}

func at(t *testing.T, st domain.Status) *domain.Claim {
	t.Helper()
	claimed := cop(t, 1000)
	return domain.Rehydrate(domain.Snapshot{ID: uuid.New(), Currency: "COP", ClaimedAmount: &claimed, Status: st, Version: 1})
}

func valid(t *testing.T) domain.Snapshot {
	t.Helper()
	return domain.Snapshot{Currency: "COP", OccurredOn: date(t, "2026-06-01"), Location: "Bogotá", ClaimType: "Choque", Description: "Choque simple"}
}

func TestNewAlertsDoNotBlock(t *testing.T) {
	t.Parallel()
	today := date(t, "2026-11-05")
	from, to := date(t, "2026-01-01"), date(t, "2026-12-31")
	cases := map[string]struct {
		occurred            string
		policy              domain.PolicyView
		outside, arrears, o bool
	}{
		"al día":           {"2026-06-01", domain.PolicyView{InForce: true, From: from, To: to}, false, false, false},
		"primer día":       {"2026-01-01", domain.PolicyView{InForce: true, From: from, To: to}, false, false, false},
		"fuera":            {"2025-12-31", domain.PolicyView{InForce: true, From: from, To: to}, true, false, false},
		"en mora":          {"2026-06-01", domain.PolicyView{InForce: true, InArrears: true, From: from, To: to}, false, true, false},
		"póliza sin vigor": {"2026-06-01", domain.PolicyView{From: from, To: to}, false, false, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := valid(t)
			s.OccurredOn = date(t, c.occurred)
			cl, err := domain.New(s, c.policy, today, now)
			require.NoError(t, err)
			got := cl.Snapshot()
			assert.Equal(t, c.outside, got.AlertOutOfCoverage)
			assert.Equal(t, c.arrears, got.AlertInArrears)
			assert.Equal(t, c.o, got.AlertNotInForce)
			assert.Equal(t, domain.StatusReported, got.Status)
		})
	}
}

func TestNewValidates(t *testing.T) {
	t.Parallel()
	today := date(t, "2026-11-05")
	policy := domain.PolicyView{InForce: true, From: today, To: today}
	s := valid(t)
	s.Location = "x"
	_, err := domain.New(s, policy, today, now)
	require.ErrorIs(t, err, domain.ErrInvalidData)
	s = valid(t)
	s.OccurredOn = today.AddDays(1)
	_, err = domain.New(s, policy, today, now)
	require.ErrorIs(t, err, domain.ErrFutureOccurrence)
	s = valid(t)
	usd, err := kernel.NewMoney(decimal.NewFromInt(1), "USD")
	require.NoError(t, err)
	s.ClaimedAmount = &usd
	_, err = domain.New(s, policy, today, now)
	require.ErrorIs(t, err, domain.ErrInvalidAmount)
}

// Recorre todas las parejas de estados contra la máquina de DOMAIN.md.
func TestTransitions(t *testing.T) {
	t.Parallel()
	all := []domain.Status{domain.StatusReported, domain.StatusNotified, domain.StatusDocumenting, domain.StatusEvaluating, domain.StatusApproved, domain.StatusObjected, domain.StatusPaid, domain.StatusClosed}
	act := map[domain.Status]func(*domain.Claim) error{
		domain.StatusNotified:    func(c *domain.Claim) error { return c.Notify("S-1", now) },
		domain.StatusDocumenting: func(c *domain.Claim) error { return c.StartDocumentation() },
		domain.StatusEvaluating:  func(c *domain.Claim) error { return c.StartEvaluation() },
		domain.StatusApproved:    func(c *domain.Claim) error { return c.Approve("Carta 123") },
		domain.StatusObjected:    func(c *domain.Claim) error { return c.Object("Carta 123") },
		domain.StatusPaid:        func(c *domain.Claim) error { return c.RecordPayment(cop(t, 1000), false) },
		domain.StatusClosed:      func(c *domain.Claim) error { return c.Close() },
	}
	valid := map[domain.Status][]domain.Status{
		domain.StatusReported:    {domain.StatusNotified},
		domain.StatusNotified:    {domain.StatusDocumenting},
		domain.StatusDocumenting: {domain.StatusEvaluating},
		domain.StatusEvaluating:  {domain.StatusApproved, domain.StatusObjected},
		domain.StatusApproved:    {domain.StatusPaid},
		domain.StatusObjected:    {domain.StatusEvaluating, domain.StatusClosed},
		domain.StatusPaid:        {domain.StatusClosed},
	}
	for _, from := range all {
		for to, fn := range act {
			c := at(t, from)
			err := fn(c)
			allowed := false
			for _, v := range valid[from] {
				allowed = allowed || v == to
			}
			if allowed {
				require.NoError(t, err, "%s → %s", from, to)
				assert.Equal(t, to, c.Snapshot().Status)
			} else {
				require.ErrorIs(t, err, domain.ErrInvalidTransition, "%s → %s", from, to)
				assert.Equal(t, from, c.Snapshot().Status)
			}
		}
	}
}

func TestDecisionRequiresSupport(t *testing.T) {
	t.Parallel()
	c := at(t, domain.StatusEvaluating)
	require.ErrorIs(t, c.Approve(" "), domain.ErrSupportRequired)
	require.ErrorIs(t, c.Object("x"), domain.ErrSupportRequired)
	assert.Equal(t, domain.StatusEvaluating, c.Snapshot().Status)
	require.NoError(t, c.Approve(" Carta 9 "))
	assert.Equal(t, "Carta 9", c.Snapshot().DecisionSupport)
}

func TestPayment(t *testing.T) {
	t.Parallel()
	require.NoError(t, at(t, domain.StatusApproved).RecordPayment(cop(t, 1000), false), "igual a lo reclamado")
	require.ErrorIs(t, at(t, domain.StatusApproved).RecordPayment(cop(t, 1001), false), domain.ErrPaidExceedsClaim)
	over := at(t, domain.StatusApproved)
	require.NoError(t, over.RecordPayment(cop(t, 1001), true))
	assert.Equal(t, domain.StatusPaid, over.Snapshot().Status)
	require.ErrorIs(t, at(t, domain.StatusApproved).RecordPayment(cop(t, 0), false), domain.ErrInvalidAmount)
	usd, err := kernel.NewMoney(decimal.NewFromInt(1), "USD")
	require.NoError(t, err)
	require.ErrorIs(t, at(t, domain.StatusApproved).RecordPayment(usd, false), domain.ErrInvalidAmount)
	none := domain.Rehydrate(domain.Snapshot{Currency: "COP", Status: domain.StatusApproved})
	require.ErrorIs(t, none.RecordPayment(cop(t, 1), false), domain.ErrClaimedRequired)
}

func TestSetClaimed(t *testing.T) {
	t.Parallel()
	require.NoError(t, at(t, domain.StatusDocumenting).SetClaimed(cop(t, 5)))
	require.ErrorIs(t, at(t, domain.StatusApproved).SetClaimed(cop(t, 5)), domain.ErrInvalidTransition)
	neg := cop(t, -1)
	require.ErrorIs(t, at(t, domain.StatusReported).SetClaimed(neg), domain.ErrInvalidAmount)
}

func TestTextValidators(t *testing.T) {
	t.Parallel()
	require.NoError(t, domain.ValidateHealth("Fractura"))
	require.ErrorIs(t, domain.ValidateHealth("x"), domain.ErrInvalidText)
	require.ErrorIs(t, domain.ValidateLabel("x"), domain.ErrInvalidText)
	require.ErrorIs(t, domain.ValidateNote("x"), domain.ErrInvalidText)
	require.ErrorIs(t, at(t, domain.StatusReported).Notify(string(make([]rune, 61)), now), domain.ErrInvalidText)
}
