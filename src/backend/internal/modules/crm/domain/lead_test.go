package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/crm/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

var now = time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // constante de prueba

func at(st domain.Status) *domain.Lead {
	return domain.Rehydrate(domain.Snapshot{ID: uuid.New(), Name: "Ana", Phone: "3001234567", Source: "call", Status: st, Version: 1})
}

func TestNewValidates(t *testing.T) {
	t.Parallel()
	ok := domain.Snapshot{ID: uuid.New(), Name: "Ana Díaz", Email: "ana@example.com", Source: "referral"}
	cases := map[string]struct {
		mut  func(*domain.Snapshot)
		want error
	}{
		"válido":        {func(*domain.Snapshot) {}, nil},
		"nombre corto":  {func(s *domain.Snapshot) { s.Name = "A" }, domain.ErrInvalidName},
		"sin contacto":  {func(s *domain.Snapshot) { s.Email = "" }, domain.ErrContactRequired},
		"correo malo":   {func(s *domain.Snapshot) { s.Email = "x" }, domain.ErrContactRequired},
		"teléfono malo": {func(s *domain.Snapshot) { s.Email, s.Phone = "", "12" }, domain.ErrContactRequired},
		"origen malo":   {func(s *domain.Snapshot) { s.Source = "x" }, domain.ErrInvalidSource},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := ok
			c.mut(&s)
			l, err := domain.New(s, now)
			if c.want != nil {
				require.ErrorIs(t, err, c.want)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, domain.StatusNew, l.Snapshot().Status)
		})
	}
}

func TestNewWithAgentRecordsAssignment(t *testing.T) {
	t.Parallel()
	l, err := domain.New(domain.Snapshot{Name: "Ana", Phone: "3001234567", Source: "call", AgentID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}, now)
	require.NoError(t, err)
	assert.Equal(t, now, *l.Snapshot().AssignedAt)
}

// Recorre todas las parejas de estados: debe coincidir con DECISIONS N31.
func TestTransitions(t *testing.T) {
	t.Parallel()
	today, err := kernel.ParseDate("2026-11-05")
	require.NoError(t, err)
	all := []domain.Status{domain.StatusNew, domain.StatusContacted, domain.StatusInterested, domain.StatusQuoting, domain.StatusWon, domain.StatusLost, domain.StatusFollowUp}
	act := map[domain.Status]func(*domain.Lead) error{
		domain.StatusContacted:  func(l *domain.Lead) error { return l.MarkContacted(now) },
		domain.StatusInterested: func(l *domain.Lead) error { return l.MarkInterested() },
		domain.StatusQuoting:    func(l *domain.Lead) error { return l.StartQuoting(uuid.New()) },
		domain.StatusWon:        func(l *domain.Lead) error { return l.MarkWon() },
		domain.StatusLost:       func(l *domain.Lead) error { return l.MarkLost("No le interesa") },
		domain.StatusFollowUp:   func(l *domain.Lead) error { return l.ScheduleFollowUp(today, today) },
	}
	valid := map[domain.Status][]domain.Status{
		domain.StatusNew:        {domain.StatusContacted, domain.StatusLost},
		domain.StatusContacted:  {domain.StatusInterested, domain.StatusFollowUp, domain.StatusLost},
		domain.StatusInterested: {domain.StatusQuoting, domain.StatusFollowUp, domain.StatusLost},
		domain.StatusQuoting:    {domain.StatusWon, domain.StatusFollowUp, domain.StatusLost},
		domain.StatusFollowUp:   {domain.StatusContacted, domain.StatusInterested, domain.StatusLost},
	}
	for _, from := range all {
		for to, fn := range act {
			l := at(from)
			err := fn(l)
			allowed := false
			for _, v := range valid[from] {
				allowed = allowed || v == to
			}
			if allowed {
				require.NoError(t, err, "%s → %s", from, to)
				assert.Equal(t, to, l.Snapshot().Status)
			} else {
				require.ErrorIs(t, err, domain.ErrInvalidTransition, "%s → %s", from, to)
				assert.Equal(t, from, l.Snapshot().Status)
			}
		}
	}
}

func TestRuleInputs(t *testing.T) {
	t.Parallel()
	today, err := kernel.ParseDate("2026-11-05")
	require.NoError(t, err)
	require.ErrorIs(t, at(domain.StatusContacted).MarkLost(" a "), domain.ErrInvalidReason)
	require.ErrorIs(t, at(domain.StatusContacted).ScheduleFollowUp(today.AddDays(-1), today), domain.ErrInvalidFollowUp)
	require.ErrorIs(t, at(domain.StatusInterested).StartQuoting(uuid.Nil), domain.ErrInvalidParty)
	require.ErrorIs(t, at(domain.StatusNew).Assign(uuid.Nil, now), domain.ErrInvalidAgent)
	require.ErrorIs(t, at(domain.StatusWon).Assign(uuid.New(), now), domain.ErrInvalidTransition)
	require.NoError(t, at(domain.StatusNew).Assign(uuid.New(), now))
}

func TestFirstContactDateIsKept(t *testing.T) {
	t.Parallel()
	today, err := kernel.ParseDate("2026-11-05")
	require.NoError(t, err)
	l := at(domain.StatusNew)
	require.NoError(t, l.MarkContacted(now))
	require.NoError(t, l.ScheduleFollowUp(today, today))
	assert.NotNil(t, l.Snapshot().FollowUpOn)
	require.NoError(t, l.MarkContacted(now.Add(time.Hour)))
	assert.Equal(t, now, *l.Snapshot().ContactedAt)
	assert.Nil(t, l.Snapshot().FollowUpOn, "salir de seguimiento limpia la fecha")
}
