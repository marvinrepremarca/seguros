// Package domaintest contiene builders de pólizas válidas para pruebas.
package domaintest

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Now es el instante fijo de las pruebas.
func Now() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }

// Money construye un monto en COP.
func Money(t *testing.T, amount string) kernel.Money {
	t.Helper()
	m, err := kernel.ParseMoney(amount, kernel.COP)
	require.NoError(t, err)
	return m
}

// Date interpreta una fecha.
func Date(t *testing.T, s string) kernel.Date {
	t.Helper()
	d, err := kernel.ParseDate(s)
	require.NoError(t, err)
	return d
}

// ValidTerms devuelve datos válidos: vigencia 2026-01-01 a 2026-12-31, prima 1.000.000 + 190.000.
func ValidTerms(t *testing.T) domain.Terms {
	t.Helper()
	period, err := kernel.NewDateRange(Date(t, "2026-01-01"), Date(t, "2026-12-31"))
	require.NoError(t, err)
	holder := uuid.New()
	return domain.Terms{
		InsurerID: uuid.New(), LineOfBusinessID: uuid.New(),
		NetPremium: Money(t, "1000000"), Taxes: Money(t, "190000"),
		Period: period, PaymentForm: domain.PaymentSingle,
		Parties:   []domain.PolicyParty{{PartyID: holder, Role: domain.RoleHolder}, {PartyID: holder, Role: domain.RoleInsured}},
		Coverages: []domain.Coverage{{ID: uuid.New(), Name: "Daños", SumInsured: Money(t, "50000000"), Deductible: "10%"}},
		Items:     []domain.InsuredItem{{ID: uuid.New(), Description: "Vehículo", RiskSchemaKey: "auto", RiskSchemaVersion: 1, RiskData: []byte(`{"plate":"ABC123"}`)}},
	}
}

// PolicyIn devuelve una póliza válida en el estado dado, con número si ya fue expedida.
func PolicyIn(t *testing.T, status domain.Status) *domain.Policy {
	t.Helper()
	p, err := domain.NewPolicy(domain.PolicyID(uuid.Must(uuid.NewV7())), ValidTerms(t), Now())
	require.NoError(t, err)
	s := p.Snapshot()
	s.Status = status
	if status != domain.StatusInQuotation && status != domain.StatusInProcess && status != domain.StatusRejected {
		s.Number = "POL-1"
	}
	return domain.Rehydrate(s)
}
