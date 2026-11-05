package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/commissions/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func accrual(t *testing.T, base kernel.Money, rate *kernel.Percentage) *domain.Accrual {
	t.Helper()
	a, err := domain.NewAccrual(domain.AccrualInput{ID: uuid.New(), PolicyID: uuid.New(), SourceID: uuid.New(), Kind: domain.KindPayment, Base: base, Percentage: rate}, pct(t, "0"))
	require.NoError(t, err)
	return a
}

func TestAccrualMissingAgreementIsZeroWithAlert(t *testing.T) {
	t.Parallel()
	s := accrual(t, cop(t, "1000"), nil).Snapshot()
	assert.True(t, s.Amount.IsZero())
	assert.True(t, s.MissingAgreement)
}

func TestReversalMirrorsTheAccrual(t *testing.T) {
	t.Parallel()
	rate := pct(t, "10")
	r, err := accrual(t, cop(t, "1000"), &rate).Reversal(uuid.New())
	require.NoError(t, err)
	s := r.Snapshot()
	assert.Equal(t, domain.KindPaymentReversal, s.Kind)
	assert.Equal(t, "-100", s.Amount.Amount().String())
	assert.Equal(t, "10", s.Percentage.String())
}

func TestReversalMarkerIsZero(t *testing.T) {
	t.Parallel()
	m, err := domain.NewReversalMarker(uuid.New(), uuid.New(), uuid.New(), "COP", pct(t, "0"), kernel.Date{})
	require.NoError(t, err)
	assert.True(t, m.Snapshot().Amount.IsZero())
}

func TestReconcile(t *testing.T) {
	t.Parallel()
	rate := pct(t, "10")
	tol := cop(t, "1000")
	statement := uuid.New()

	a := accrual(t, cop(t, "50000"), &rate) // causa 5000
	ok, expected, err := domain.Reconcile([]*domain.Accrual{a}, statement, cop(t, "6000.01"), tol)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "5000", expected.Amount().String())
	assert.False(t, a.Snapshot().StatementID.Valid, "con diferencia no se concilia")

	ok, _, err = domain.Reconcile([]*domain.Accrual{a}, statement, cop(t, "4000"), tol)
	require.NoError(t, err)
	assert.True(t, ok, "justo en la tolerancia")
	assert.True(t, a.Snapshot().StatementID.Valid)

	require.ErrorIs(t, a.MarkReconciled(statement), domain.ErrAlreadyReconciled)
	_, _, err = domain.Reconcile([]*domain.Accrual{a}, statement, cop(t, "5000"), tol)
	require.ErrorIs(t, err, domain.ErrAlreadyReconciled)
}

func TestReconcileExactOutsideCOP(t *testing.T) {
	t.Parallel()
	usd := func(s string) kernel.Money {
		m, err := kernel.ParseMoney(s, "USD")
		require.NoError(t, err)
		return m
	}
	rate := pct(t, "10")
	a := accrual(t, usd("1000"), &rate)
	ok, _, err := domain.Reconcile([]*domain.Accrual{a}, uuid.New(), usd("99"), usd("0"))
	require.NoError(t, err)
	assert.False(t, ok)
	ok, _, err = domain.Reconcile([]*domain.Accrual{a}, uuid.New(), usd("100"), usd("0"))
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestToleranceRules(t *testing.T) {
	t.Parallel()
	c, err := domain.DefaultTolerance("COP")
	require.NoError(t, err)
	assert.Equal(t, "1000", c.Amount().String())
	u, err := domain.DefaultTolerance("USD")
	require.NoError(t, err)
	assert.True(t, u.IsZero(), "otra moneda: coincidencia exacta")
	require.NoError(t, domain.ValidateTolerance(cop(t, "400000")))
	require.ErrorIs(t, domain.ValidateTolerance(cop(t, "400000.01")), domain.ErrInvalidTolerance)
	require.ErrorIs(t, domain.ValidateTolerance(cop(t, "-1")), domain.ErrInvalidTolerance)
	over, err := kernel.ParseMoney("100.01", "USD")
	require.NoError(t, err)
	require.ErrorIs(t, domain.ValidateTolerance(over), domain.ErrInvalidTolerance)
	a := accrual(t, cop(t, "1"), nil)
	assert.Equal(t, a.Snapshot(), domain.RehydrateAccrual(a.Snapshot()).Snapshot())
}

func TestCurrencyMismatchIsAnError(t *testing.T) {
	t.Parallel()
	usd, err := kernel.ParseMoney("5", "USD")
	require.NoError(t, err)
	_, err = domain.NetShareOfPayment(cop(t, "1"), cop(t, "1"), usd)
	require.Error(t, err)
	_, _, err = domain.Reconcile([]*domain.Accrual{accrual(t, cop(t, "1"), nil)}, uuid.New(), usd, usd)
	require.Error(t, err)
	_, _, err = domain.Reconcile(nil, uuid.New(), usd, cop(t, "1"))
	require.Error(t, err)
}
