package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/quoting/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

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

func open(t *testing.T) *domain.Request {
	t.Helper()
	r, err := domain.NewRequest(domain.Snapshot{ID: uuid.New(), HolderPartyID: uuid.New(), RiskDescription: "Auto ABC123", RiskData: []byte(`{}`)})
	require.NoError(t, err)
	return r
}

func offer(t *testing.T) domain.Offer {
	t.Helper()
	return domain.Offer{
		ID: uuid.New(), InsurerID: uuid.New(), NetPremium: cop(t, 1000), Taxes: cop(t, 190),
		EffectiveFrom: date(t, "2027-01-01"), EffectiveTo: date(t, "2027-12-31"),
		Coverages: []domain.Coverage{{Name: "Daños", SumInsured: cop(t, 5000)}},
	}
}

func TestNewRequestValidates(t *testing.T) {
	t.Parallel()
	_, err := domain.NewRequest(domain.Snapshot{HolderPartyID: uuid.New(), RiskDescription: "x", RiskData: []byte(`{}`)})
	require.ErrorIs(t, err, domain.ErrInvalidRisk)
	_, err = domain.NewRequest(domain.Snapshot{HolderPartyID: uuid.New(), RiskDescription: "Auto"})
	require.ErrorIs(t, err, domain.ErrInvalidRisk)
	_, err = domain.NewRequest(domain.Snapshot{RiskDescription: "Auto", RiskData: []byte(`{}`)})
	require.ErrorIs(t, err, domain.ErrInvalidParty)
}

func TestAddOffer(t *testing.T) {
	t.Parallel()
	today := date(t, "2026-11-05")
	o, err := open(t).AddOffer(offer(t), today, 30)
	require.NoError(t, err)
	assert.Equal(t, date(t, "2026-12-05"), o.ValidUntil, "T3: por defecto, días del tenant")

	cases := map[string]func(*domain.Offer){
		"sin aseguradora": func(o *domain.Offer) { o.InsurerID = uuid.Nil },
		"vigencia":        func(o *domain.Offer) { o.EffectiveTo = o.EffectiveFrom },
		"moneda cobertura": func(o *domain.Offer) {
			usd, err := kernel.NewMoney(decimal.NewFromInt(1), "USD")
			require.NoError(t, err)
			o.Coverages[0].SumInsured = usd
		},
		"nombre cobertura": func(o *domain.Offer) { o.Coverages[0].Name = "x" },
		"impuestos negativos": func(o *domain.Offer) {
			neg, err := kernel.NewMoney(decimal.NewFromInt(-1), "COP")
			require.NoError(t, err)
			o.Taxes = neg
		},
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			o := offer(t)
			mut(&o)
			_, err := open(t).AddOffer(o, today, 30)
			require.ErrorIs(t, err, domain.ErrInvalidOffer)
		})
	}
	past := offer(t)
	past.ValidUntil = today.AddDays(-1)
	_, err = open(t).AddOffer(past, today, 30)
	require.ErrorIs(t, err, domain.ErrInvalidValidity)
}

func TestAcceptDiscardsTheRest(t *testing.T) {
	t.Parallel()
	today := date(t, "2026-11-05")
	r := open(t)
	a, err := r.AddOffer(offer(t), today, 30)
	require.NoError(t, err)
	b, err := r.AddOffer(offer(t), today, 30)
	require.NoError(t, err)
	policy := uuid.New()
	got, err := r.Accept(b.ID, policy, today)
	require.NoError(t, err)
	assert.Equal(t, b.ID, got.ID)
	s := r.Snapshot()
	assert.Equal(t, domain.RequestAccepted, s.Status)
	assert.Equal(t, policy, s.PolicyID.UUID)
	assert.Equal(t, domain.OfferDiscarded, s.Offers[0].Status)
	assert.Equal(t, a.ID, s.Offers[0].ID)
	assert.Equal(t, domain.OfferAccepted, s.Offers[1].Status)

	_, err = r.Accept(a.ID, uuid.New(), today)
	require.ErrorIs(t, err, domain.ErrNotOpen)
	_, err = r.AddOffer(offer(t), today, 30)
	require.ErrorIs(t, err, domain.ErrNotOpen)
	require.ErrorIs(t, r.Decline("No quiere"), domain.ErrNotOpen)
}

func TestAcceptChecksValidity(t *testing.T) {
	t.Parallel()
	today := date(t, "2026-11-05")
	r := open(t)
	o := offer(t)
	o.ValidUntil = today
	o, err := r.AddOffer(o, today, 30)
	require.NoError(t, err)
	_, err = r.Accept(uuid.New(), uuid.New(), today)
	require.ErrorIs(t, err, domain.ErrOfferNotFound)
	_, err = r.Accept(o.ID, uuid.New(), today.AddDays(1))
	require.ErrorIs(t, err, domain.ErrOfferExpired, "un día después de ValidUntil")
	_, err = r.Accept(o.ID, uuid.New(), today)
	require.NoError(t, err, "ValidUntil vale inclusive")
}

func TestDecline(t *testing.T) {
	t.Parallel()
	r := open(t)
	require.ErrorIs(t, r.Decline(" a "), domain.ErrInvalidReason)
	require.NoError(t, r.Decline("Precio alto"))
	assert.Equal(t, domain.RequestDeclined, r.Snapshot().Status)
}

func TestParameters(t *testing.T) {
	t.Parallel()
	require.NoError(t, domain.ValidatePaymentForm("monthly_installments"))
	require.ErrorIs(t, domain.ValidatePaymentForm("x"), domain.ErrInvalidPayment)
	require.NoError(t, domain.ValidateOfferValidDays(365))
	require.ErrorIs(t, domain.ValidateOfferValidDays(0), domain.ErrInvalidDays)
}
