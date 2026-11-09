package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/compliance/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func day(t *testing.T, s string) kernel.Date {
	t.Helper()
	d, err := kernel.ParseDate(s)
	require.NoError(t, err)
	return d
}

func TestNewKycCheck(t *testing.T) {
	t.Parallel()
	today := day(t, "2026-11-05")
	ok := domain.KycCheck{ID: uuid.New(), PartyID: uuid.New(), Fingerprint: []byte{1}, Result: domain.ResultApproved, Source: "Lista ONU", CheckedOn: today}
	_, err := domain.NewKycCheck(ok, today)
	require.NoError(t, err)
	cases := map[string]func(*domain.KycCheck){
		"resultado":     func(c *domain.KycCheck) { c.Result = "x" },
		"fuente":        func(c *domain.KycCheck) { c.Source = " " },
		"futura":        func(c *domain.KycCheck) { c.CheckedOn = today.AddDays(1) },
		"sin fecha":     func(c *domain.KycCheck) { c.CheckedOn = kernel.Date{} },
		"sin documento": func(c *domain.KycCheck) { c.Fingerprint = nil },
	}
	for name, mut := range cases {
		c := ok
		mut(&c)
		_, err := domain.NewKycCheck(c, today)
		require.ErrorIs(t, err, domain.ErrInvalidCheck, name)
	}
}

func TestApprovesOnlyCurrentDocument(t *testing.T) {
	t.Parallel()
	c := domain.KycCheck{Result: domain.ResultApproved, Fingerprint: []byte{1, 2}}
	assert.True(t, c.Approves([]byte{1, 2}))
	assert.False(t, c.Approves([]byte{9, 9}), "documento rectificado")
	assert.False(t, c.Approves(nil))
	c.Result = domain.ResultRejected
	assert.False(t, c.Approves([]byte{1, 2}))
}

func TestComplaint(t *testing.T) {
	t.Parallel()
	today := day(t, "2026-11-05")
	s := domain.ComplaintData{Channel: "Correo", Subject: "Demora", Description: "Sin respuesta", ReceivedOn: today}
	c, err := domain.NewComplaint(s, today)
	require.NoError(t, err)
	require.ErrorIs(t, c.Resolve("x"), domain.ErrResolutionNeeded)
	require.NoError(t, c.Start())
	require.ErrorIs(t, c.Start(), domain.ErrInvalidTransition)
	require.NoError(t, c.Resolve("Atendida"))
	require.ErrorIs(t, c.Resolve("Otra vez"), domain.ErrInvalidTransition)
	require.ErrorIs(t, c.Start(), domain.ErrInvalidTransition)
	direct, err := domain.NewComplaint(s, today)
	require.NoError(t, err)
	require.NoError(t, direct.Resolve("Atendida"))
	s.ReceivedOn = today.AddDays(1)
	_, err = domain.NewComplaint(s, today)
	require.ErrorIs(t, err, domain.ErrInvalidComplaint)
	require.ErrorIs(t, domain.ValidateResolution(" "), domain.ErrResolutionNeeded)
}
