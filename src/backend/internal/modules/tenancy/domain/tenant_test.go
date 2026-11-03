package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

var t0 = time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)

func newID() kernel.TenantID { return kernel.TenantID(uuid.Must(uuid.NewV7())) }

func validTenant(t *testing.T) *domain.Tenant {
	t.Helper()
	tn, err := domain.NewTenant(newID(), "Agencia Uno", domain.PlanBasic, "agencia-uno", t0)
	require.NoError(t, err)
	return tn
}

func TestNewTenant(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		id      kernel.TenantID
		tName   string
		plan    domain.Plan
		alias   string
		wantErr error
	}{
		{"válido", newID(), "  Agencia Uno  ", domain.PlanProfessional, "agencia-uno", nil},
		{"alias de un carácter", newID(), "A", domain.PlanBasic, "a", nil},
		{"id nulo", kernel.TenantID{}, "A", domain.PlanBasic, "a", kernel.ErrInvalidID},
		{"nombre vacío", newID(), "   ", domain.PlanBasic, "a", domain.ErrInvalidName},
		{"nombre largo", newID(), strings.Repeat("ñ", 121), domain.PlanBasic, "a", domain.ErrInvalidName},
		{"nombre de 120 runas", newID(), strings.Repeat("ñ", 120), domain.PlanBasic, "a", nil},
		{"plan desconocido", newID(), "A", domain.Plan("gratis"), "a", domain.ErrInvalidPlan},
		{"alias con mayúsculas", newID(), "A", domain.PlanBasic, "Agencia", domain.ErrInvalidOrgAlias},
		{"alias que termina en guion", newID(), "A", domain.PlanBasic, "agencia-", domain.ErrInvalidOrgAlias},
		{"alias vacío", newID(), "A", domain.PlanBasic, "", domain.ErrInvalidOrgAlias},
		{"alias largo", newID(), "A", domain.PlanBasic, strings.Repeat("a", 64), domain.ErrInvalidOrgAlias},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tn, err := domain.NewTenant(tt.id, tt.tName, tt.plan, tt.alias, t0)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, tn)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.id, tn.ID())
			assert.Equal(t, strings.TrimSpace(tt.tName), tn.Name())
			assert.Equal(t, tt.plan, tn.Plan())
			assert.Equal(t, tt.alias, tn.OrgAlias())
			assert.Equal(t, domain.StatusActive, tn.Status())
			assert.True(t, tn.IsActive())
			assert.Equal(t, int32(1), tn.Version())
		})
	}
}

func TestParsePlan(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"basico", "profesional", "empresarial"} {
		got, err := domain.ParsePlan(p)
		require.NoError(t, err)
		assert.Equal(t, domain.Plan(p), got)
	}
	_, err := domain.ParsePlan("Basico")
	require.ErrorIs(t, err, domain.ErrInvalidPlan)
}

func TestStatusTransitions(t *testing.T) {
	t.Parallel()
	later := t0.Add(time.Hour)

	t.Run("activo → suspendido → activo", func(t *testing.T) {
		t.Parallel()
		tn := validTenant(t)
		require.NoError(t, tn.Suspend(later))
		assert.Equal(t, domain.StatusSuspended, tn.Status())
		assert.False(t, tn.IsActive())
		assert.Equal(t, later, tn.Snapshot().StatusSince)
		require.NoError(t, tn.Reactivate(later.Add(time.Hour)))
		assert.True(t, tn.IsActive())
	})
	t.Run("suspender un suspendido", func(t *testing.T) {
		t.Parallel()
		tn := validTenant(t)
		require.NoError(t, tn.Suspend(later))
		require.ErrorIs(t, tn.Suspend(later), domain.ErrInvalidTransition)
	})
	t.Run("reactivar un activo", func(t *testing.T) {
		t.Parallel()
		tn := validTenant(t)
		err := tn.Reactivate(later)
		require.ErrorIs(t, err, domain.ErrInvalidTransition)
		assert.Equal(t, t0, tn.Snapshot().UpdatedAt, "una transición inválida no modifica el agregado")
	})
}

func TestChangePlan(t *testing.T) {
	t.Parallel()
	tn := validTenant(t)
	later := t0.Add(time.Hour)
	require.NoError(t, tn.ChangePlan(domain.PlanEnterprise, later))
	assert.Equal(t, domain.PlanEnterprise, tn.Plan())
	assert.Equal(t, later, tn.Snapshot().UpdatedAt)

	require.ErrorIs(t, tn.ChangePlan("otro", later), domain.ErrInvalidPlan)
	assert.Equal(t, domain.PlanEnterprise, tn.Plan())

	require.NoError(t, tn.ChangePlan(domain.PlanEnterprise, later.Add(time.Hour)))
	assert.Equal(t, later, tn.Snapshot().UpdatedAt, "mismo plan: sin cambios")
}

func TestSnapshotRoundTrip(t *testing.T) {
	t.Parallel()
	tn := validTenant(t)
	require.NoError(t, tn.Suspend(t0.Add(time.Minute)))
	again := domain.RehydrateTenant(tn.Snapshot())
	assert.Equal(t, tn.Snapshot(), again.Snapshot())
}
