package domain_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/billing/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func newID() (uuid.UUID, error) { return uuid.NewV7() }

func d(t *testing.T, s string) kernel.Date {
	t.Helper()
	v, err := kernel.ParseDate(s)
	require.NoError(t, err)
	return v
}

func cop(t *testing.T, s string) kernel.Money {
	t.Helper()
	m, err := kernel.ParseMoney(s, "COP")
	require.NoError(t, err)
	return m
}

func plan(t *testing.T, form, total, from, to string) *domain.PaymentPlan {
	t.Helper()
	p, err := domain.NewPaymentPlan(domain.PlanInput{
		ID: uuid.Must(newID()), PolicyID: uuid.Must(newID()), PolicyNumber: "P-1", InsurerID: uuid.Must(newID()), HolderPartyID: uuid.Must(newID()),
		PaymentForm: form, Total: cop(t, total), From: d(t, from), To: d(t, to), NewID: newID,
	})
	require.NoError(t, err)
	return p
}

func amounts(p *domain.PaymentPlan) []string {
	var out []string
	for _, i := range p.Snapshot().Installments {
		out = append(out, i.Amount.Amount().String())
	}
	return out
}

func sum(t *testing.T, p *domain.PaymentPlan) string {
	t.Helper()
	s := cop(t, "0")
	var err error
	for _, i := range p.Snapshot().Installments {
		s, err = s.Add(i.Amount)
		require.NoError(t, err)
	}
	return s.Amount().String()
}

func TestSchedule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, form, from, to string
		dues                 []string
	}{
		{"único", "single_payment", "2026-01-15", "2027-01-14", []string{"2026-01-15"}},
		{"financiación", "premium_financing", "2026-01-15", "2027-01-14", []string{"2026-01-15"}},
		{"trimestral", "quarterly_installments", "2026-01-15", "2027-01-14", []string{"2026-01-15", "2026-04-15", "2026-07-15", "2026-10-15"}},
		{"mensual ajusta al fin de mes sin arrastrar", "monthly_installments", "2026-01-31", "2026-04-30", []string{"2026-01-31", "2026-02-28", "2026-03-31", "2026-04-30"}},
		{"débito es mensual", "automatic_debit", "2026-01-01", "2026-02-28", []string{"2026-01-01", "2026-02-01"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, i := range plan(t, tt.form, "1000", tt.from, tt.to).Snapshot().Installments {
				got = append(got, i.DueDate.String())
			}
			assert.Equal(t, tt.dues, got)
		})
	}
}

func TestNewPlanInvalid(t *testing.T) {
	t.Parallel()
	base := domain.PlanInput{PaymentForm: "single_payment", Total: cop(t, "10"), From: d(t, "2026-01-01"), To: d(t, "2026-12-31"), NewID: newID}
	bad := base
	bad.PaymentForm = "x"
	_, err := domain.NewPaymentPlan(bad)
	require.ErrorIs(t, err, domain.ErrInvalidPaymentForm)
	bad = base
	bad.To = d(t, "2025-01-01")
	_, err = domain.NewPaymentPlan(bad)
	require.ErrorIs(t, err, domain.ErrInvalidPeriod)
	bad = base
	bad.Total = cop(t, "-1")
	_, err = domain.NewPaymentPlan(bad)
	require.ErrorIs(t, err, domain.ErrNegativeTotal)
}

func TestResidueGoesToLastInstallment(t *testing.T) {
	t.Parallel()
	p := plan(t, "monthly_installments", "100000", "2026-01-01", "2026-03-31")
	assert.Equal(t, []string{"33333.33", "33333.33", "33333.34"}, amounts(p))
}

func TestZeroPremiumHasNoInstallments(t *testing.T) {
	t.Parallel()
	assert.Empty(t, plan(t, "monthly_installments", "0", "2026-01-01", "2026-03-31").Snapshot().Installments)
}

func TestPaymentFIFOAndCredit(t *testing.T) {
	t.Parallel()
	p := plan(t, "monthly_installments", "300", "2026-01-01", "2026-03-31")
	allocs, surplus, err := p.ApplyPayment(cop(t, "150"))
	require.NoError(t, err)
	require.Len(t, allocs, 2)
	assert.True(t, surplus.IsZero())
	s := p.Snapshot().Installments
	assert.Equal(t, domain.InstallmentPaid, s[0].Status)
	assert.Equal(t, domain.InstallmentPending, s[1].Status)
	assert.Equal(t, "50", s[1].Paid.Amount().String())

	_, surplus, err = p.ApplyPayment(cop(t, "200"))
	require.NoError(t, err)
	assert.Equal(t, "50", surplus.Amount().String(), "sobrante final: saldo a favor")
	assert.Equal(t, "50", p.Snapshot().Credit.Amount().String())

	_, _, err = p.ApplyPayment(cop(t, "0"))
	require.ErrorIs(t, err, domain.ErrInvalidPayment)
	usd, err := kernel.ParseMoney("5", "USD")
	require.NoError(t, err)
	_, _, err = p.ApplyPayment(usd)
	require.ErrorIs(t, err, domain.ErrInvalidPayment)
}

func TestReverseRestoresBalanceAndOverdueNotice(t *testing.T) {
	t.Parallel()
	p := plan(t, "single_payment", "100", "2026-01-01", "2026-12-31")
	require.Len(t, p.MarkOverdue(d(t, "2026-02-01"), 30), 1)
	allocs, surplus, err := p.ApplyPayment(cop(t, "120"))
	require.NoError(t, err)
	assert.False(t, p.InArrears(d(t, "2026-02-01"), 30))

	require.NoError(t, p.ReversePayment(allocs, surplus))
	s := p.Snapshot()
	assert.True(t, s.Credit.IsZero())
	assert.Equal(t, domain.InstallmentPending, s.Installments[0].Status)
	assert.True(t, p.InArrears(d(t, "2026-02-01"), 30))
	assert.Len(t, p.MarkOverdue(d(t, "2026-02-01"), 30), 1, "se vuelve a avisar")
}

func TestOverdueBoundaryAndNoticeOnce(t *testing.T) {
	t.Parallel()
	p := plan(t, "single_payment", "100", "2026-01-01", "2026-12-31")
	assert.Empty(t, p.MarkOverdue(d(t, "2026-01-30"), 30), "día 29")
	assert.False(t, p.InArrears(d(t, "2026-01-30"), 30))
	assert.Len(t, p.MarkOverdue(d(t, "2026-01-31"), 30), 1, "día 30")
	assert.Empty(t, p.MarkOverdue(d(t, "2026-02-01"), 30), "una sola vez")
}

func TestLeavesArrearsWithOtherInstallmentsInsideGrace(t *testing.T) {
	t.Parallel()
	p := plan(t, "monthly_installments", "200", "2026-01-01", "2026-02-28")
	today := d(t, "2026-02-05") // la 1 fuera de gracia; la 2 vencida dentro de gracia
	assert.True(t, p.InArrears(today, 30))
	_, _, err := p.ApplyPayment(cop(t, "100"))
	require.NoError(t, err)
	assert.False(t, p.InArrears(today, 30), "N19")
}

func TestEndorsement(t *testing.T) {
	t.Parallel()
	eff := "2026-02-15"
	t.Run("aumento en pendientes con residuo en la última", func(t *testing.T) {
		t.Parallel()
		p := plan(t, "monthly_installments", "300", "2026-01-01", "2026-03-31")
		require.NoError(t, p.ApplyEndorsement(cop(t, "100"), d(t, eff), newID))
		assert.Equal(t, []string{"133.33", "133.33", "133.34"}, amounts(p))
		assert.Equal(t, "400", sum(t, p))
		assert.Equal(t, "400", p.Snapshot().Total.Amount().String())
	})
	t.Run("disminución no toca lo pagado", func(t *testing.T) {
		t.Parallel()
		p := plan(t, "monthly_installments", "300", "2026-01-01", "2026-03-31")
		_, _, err := p.ApplyPayment(cop(t, "100"))
		require.NoError(t, err)
		require.NoError(t, p.ApplyEndorsement(cop(t, "-50"), d(t, eff), newID))
		assert.Equal(t, []string{"100", "75", "75"}, amounts(p))
	})
	t.Run("disminución mayor que el saldo va a saldo a favor", func(t *testing.T) {
		t.Parallel()
		p := plan(t, "monthly_installments", "300", "2026-01-01", "2026-03-31")
		_, _, err := p.ApplyPayment(cop(t, "150"))
		require.NoError(t, err)
		require.NoError(t, p.ApplyEndorsement(cop(t, "-200"), d(t, eff), newID))
		s := p.Snapshot()
		assert.Equal(t, "50", s.Credit.Amount().String())
		assert.Equal(t, domain.InstallmentPaid, s.Installments[1].Status)
		assert.Equal(t, "50", s.Installments[1].Amount.Amount().String())
		assert.True(t, s.Installments[2].Amount.IsZero())
	})
	t.Run("sin pendientes: aumento crea cuota", func(t *testing.T) {
		t.Parallel()
		p := plan(t, "single_payment", "100", "2026-01-01", "2026-12-31")
		_, _, err := p.ApplyPayment(cop(t, "100"))
		require.NoError(t, err)
		require.NoError(t, p.ApplyEndorsement(cop(t, "40"), d(t, eff), newID))
		s := p.Snapshot().Installments
		require.Len(t, s, 2)
		assert.Equal(t, eff, s[1].DueDate.String())
		assert.Equal(t, "40", s[1].Amount.Amount().String())
	})
	t.Run("sin pendientes: disminución es saldo a favor", func(t *testing.T) {
		t.Parallel()
		p := plan(t, "single_payment", "100", "2026-01-01", "2026-12-31")
		_, _, err := p.ApplyPayment(cop(t, "100"))
		require.NoError(t, err)
		require.NoError(t, p.ApplyEndorsement(cop(t, "-30"), d(t, eff), newID))
		assert.Equal(t, "30", p.Snapshot().Credit.Amount().String())
	})
	t.Run("redondeo en disminución respeta el saldo", func(t *testing.T) {
		t.Parallel()
		p := plan(t, "monthly_installments", "100", "2026-01-01", "2026-03-31")
		require.NoError(t, p.ApplyEndorsement(cop(t, "-99.99"), d(t, eff), newID))
		assert.Equal(t, "0.01", sum(t, p))
		for _, i := range p.Snapshot().Installments {
			assert.False(t, i.Amount.IsNegative())
		}
	})
	t.Run("no deja total negativo", func(t *testing.T) {
		t.Parallel()
		p := plan(t, "single_payment", "100", "2026-01-01", "2026-12-31")
		require.ErrorIs(t, p.ApplyEndorsement(cop(t, "-101"), d(t, eff), newID), domain.ErrNegativeTotal)
	})
}

func TestAdjustSchedule(t *testing.T) {
	t.Parallel()
	p := plan(t, "monthly_installments", "300", "2026-01-01", "2026-03-31")
	_, _, err := p.ApplyPayment(cop(t, "50"))
	require.NoError(t, err)

	bad := []domain.ScheduledInstallment{{DueDate: d(t, "2026-05-01"), Amount: cop(t, "150")}}
	require.ErrorIs(t, p.AdjustSchedule(bad, newID), domain.ErrScheduleMismatch)
	require.ErrorIs(t, p.AdjustSchedule([]domain.ScheduledInstallment{{DueDate: d(t, "2026-05-01")}}, newID), domain.ErrInvalidInstallment)

	require.NoError(t, p.AdjustSchedule([]domain.ScheduledInstallment{
		{DueDate: d(t, "2026-06-01"), Amount: cop(t, "120")}, {DueDate: d(t, "2026-05-01"), Amount: cop(t, "80")},
	}, newID))
	s := p.Snapshot().Installments
	require.Len(t, s, 3)
	assert.Equal(t, "50", s[0].Paid.Amount().String(), "lo abonado se conserva")
	assert.Equal(t, "2026-05-01", s[1].DueDate.String())
	assert.Equal(t, int32(3), s[2].Number)
	assert.Equal(t, "300", sum(t, p))
}

func TestCancel(t *testing.T) {
	t.Parallel()
	p := plan(t, "monthly_installments", "300", "2026-01-01", "2026-03-31")
	_, _, err := p.ApplyPayment(cop(t, "150"))
	require.NoError(t, err)
	require.NoError(t, p.Cancel(d(t, "2026-02-01"), cop(t, "80")))
	require.NoError(t, p.Cancel(d(t, "2026-03-01"), cop(t, "1")), "repetir no tiene efecto")
	s := p.Snapshot()
	assert.Equal(t, domain.InstallmentPaid, s.Installments[0].Status)
	assert.Equal(t, domain.InstallmentVoided, s.Installments[1].Status)
	assert.Equal(t, "50", s.Installments[1].Paid.Amount().String(), "los pagos no se tocan")
	assert.Equal(t, "80", s.Refund.Amount.Amount().String())
	assert.True(t, s.Credit.IsZero(), "la devolución no es saldo a favor")
	assert.False(t, p.InArrears(d(t, "2027-01-01"), 30))

	_, _, err = p.ApplyPayment(cop(t, "1"))
	require.ErrorIs(t, err, domain.ErrPlanCancelled)
	require.ErrorIs(t, p.ApplyEndorsement(cop(t, "1"), d(t, "2026-02-01"), newID), domain.ErrPlanCancelled)
	require.ErrorIs(t, p.AdjustSchedule(nil, newID), domain.ErrPlanCancelled)
}

func TestValidateGraceDays(t *testing.T) {
	t.Parallel()
	require.NoError(t, domain.ValidateGraceDays(0))
	require.NoError(t, domain.ValidateGraceDays(365))
	require.ErrorIs(t, domain.ValidateGraceDays(-1), domain.ErrInvalidGraceDays)
	require.ErrorIs(t, domain.ValidateGraceDays(366), domain.ErrInvalidGraceDays)
}

func TestValidatePaymentDate(t *testing.T) {
	t.Parallel()
	p := plan(t, "single_payment", "100", "2026-03-01", "2026-12-31")
	today := d(t, "2026-04-01")
	require.NoError(t, p.ValidatePaymentDate(today, today, 60))
	require.NoError(t, p.ValidatePaymentDate(d(t, "2025-12-31"), today, 60), "justo 60 días antes (N22)")
	require.ErrorIs(t, p.ValidatePaymentDate(d(t, "2025-12-30"), today, 60), domain.ErrPaymentDateRange)
	require.NoError(t, p.ValidatePaymentDate(d(t, "2025-12-30"), today, 61), "parámetro de la agencia")
	require.ErrorIs(t, p.ValidatePaymentDate(d(t, "2026-04-02"), today, 60), domain.ErrFuturePayment)
	require.NoError(t, plan(t, "single_payment", "0", "2026-03-01", "2026-12-31").ValidatePaymentDate(d(t, "2020-01-01"), today, 60), "sin cuotas")
}

func TestRehydrateKeepsState(t *testing.T) {
	t.Parallel()
	p := plan(t, "monthly_installments", "300", "2026-01-01", "2026-03-31")
	assert.Equal(t, p.Snapshot(), domain.RehydratePlan(p.Snapshot()).Snapshot())
}

func TestMixedCurrencyIsRejected(t *testing.T) {
	t.Parallel()
	usd, err := kernel.ParseMoney("5", "USD")
	require.NoError(t, err)
	p := plan(t, "single_payment", "100", "2026-01-01", "2026-12-31")
	require.Error(t, p.ApplyEndorsement(usd, d(t, "2026-02-01"), newID))
	require.ErrorIs(t, p.AdjustSchedule([]domain.ScheduledInstallment{{DueDate: d(t, "2026-02-01"), Amount: usd}}, newID), domain.ErrInvalidInstallment)
}

func TestEndorsementFailingIDGeneratorPropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("sin ids")
	failing := func() (uuid.UUID, error) { return uuid.Nil, boom }
	p := plan(t, "single_payment", "100", "2026-01-01", "2026-12-31")
	_, _, err := p.ApplyPayment(cop(t, "100"))
	require.NoError(t, err)
	require.ErrorIs(t, p.ApplyEndorsement(cop(t, "1"), d(t, "2026-02-01"), failing), boom)
	require.ErrorIs(t, p.AdjustSchedule([]domain.ScheduledInstallment{{DueDate: d(t, "2026-02-01"), Amount: cop(t, "1")}}, failing), boom)
}

// Un plan con monedas mezcladas en BD no debe aplicarse a medias: cada operación devuelve error.
func TestCorruptedCurrenciesFailClosed(t *testing.T) {
	t.Parallel()
	usd, err := kernel.ParseMoney("5", "USD")
	require.NoError(t, err)
	build := func(mutate func(*domain.PlanSnapshot)) *domain.PaymentPlan {
		s := plan(t, "monthly_installments", "300", "2026-01-01", "2026-03-31").Snapshot()
		mutate(&s)
		return domain.RehydratePlan(s)
	}
	paidUSD := func(s *domain.PlanSnapshot) { s.Installments[0].Paid = usd }
	amountUSD := func(s *domain.PlanSnapshot) { s.Installments[1].Amount = usd }
	creditUSD := func(s *domain.PlanSnapshot) { s.Credit = usd }
	eff := d(t, "2026-02-01")
	for name, mutate := range map[string]func(*domain.PlanSnapshot){"abonado": paidUSD, "valor": amountUSD} {
		_, _, err := build(mutate).ApplyPayment(cop(t, "1000"))
		require.Error(t, err, name)
		require.Error(t, build(mutate).ApplyEndorsement(cop(t, "10"), eff, newID), name)
		require.Error(t, build(mutate).ApplyEndorsement(cop(t, "-10"), eff, newID), name)
	}
	_, _, err = build(creditUSD).ApplyPayment(cop(t, "1000"))
	require.Error(t, err)
	require.Error(t, build(creditUSD).ReversePayment(nil, cop(t, "1")))
	require.Error(t, build(amountUSD).AdjustSchedule(nil, newID))
	require.Error(t, build(creditUSD).ApplyEndorsement(cop(t, "-1000"), eff, newID))
}

func TestReverseIgnoresUnknownInstallmentAndRejectsMixedAmount(t *testing.T) {
	t.Parallel()
	usd, err := kernel.ParseMoney("5", "USD")
	require.NoError(t, err)
	p := plan(t, "single_payment", "100", "2026-01-01", "2026-12-31")
	id := p.Snapshot().Installments[0].ID
	require.NoError(t, p.ReversePayment([]domain.Allocation{{InstallmentID: uuid.New(), Amount: cop(t, "1")}}, cop(t, "0")))
	require.Error(t, p.ReversePayment([]domain.Allocation{{InstallmentID: id, Amount: usd}}, cop(t, "0")))
}

func TestReverseOnCancelledPlanDoesNotReopenReceivable(t *testing.T) {
	t.Parallel()
	p := plan(t, "single_payment", "100", "2026-01-01", "2026-12-31")
	allocs, surplus, err := p.ApplyPayment(cop(t, "100"))
	require.NoError(t, err)
	require.NoError(t, p.Cancel(d(t, "2026-02-01"), cop(t, "0")))
	require.NoError(t, p.ReversePayment(allocs, surplus))
	i := p.Snapshot().Installments[0]
	assert.Equal(t, domain.InstallmentVoided, i.Status)
	assert.True(t, i.Paid.IsZero())
	assert.False(t, p.InArrears(d(t, "2027-01-01"), 30))
}
