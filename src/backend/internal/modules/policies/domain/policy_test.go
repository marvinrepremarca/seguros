package domain_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	dt "github.com/marvinrepremarca/seguros/internal/modules/policies/domain/domaintest"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func allStatuses() []domain.Status {
	return []domain.Status{
		domain.StatusInQuotation, domain.StatusInProcess, domain.StatusActive, domain.StatusInArrears, domain.StatusCancelled,
		domain.StatusExpired, domain.StatusRenewed, domain.StatusNotRenewed, domain.StatusRejected,
	}
}

func cancellation(t *testing.T) domain.Cancellation {
	t.Helper()
	return domain.Cancellation{Cause: domain.CauseClientRequest, EffectiveDate: dt.Date(t, "2026-06-30"), UnearnedPremium: dt.Money(t, "500000")}
}

// Todas las operaciones contra todos los estados: la única combinación permitida es la de DOMAIN.md.
func TestStateMachineIsExhaustive(t *testing.T) {
	t.Parallel()
	ops := []struct {
		name string
		from []domain.Status
		to   domain.Status
		run  func(t *testing.T, p *domain.Policy) error
	}{
		{"Accept", []domain.Status{domain.StatusInQuotation}, domain.StatusInProcess, func(_ *testing.T, p *domain.Policy) error { return p.Accept(dt.Now()) }},
		{"Issue", []domain.Status{domain.StatusInProcess}, domain.StatusActive, func(_ *testing.T, p *domain.Policy) error {
			return p.Issue("POL-9", domain.KycNotRequired, dt.Now())
		}},
		{"Reject", []domain.Status{domain.StatusInProcess}, domain.StatusRejected, func(_ *testing.T, p *domain.Policy) error { return p.Reject("sin capacidad", dt.Now()) }},
		{"MarkInArrears", []domain.Status{domain.StatusActive}, domain.StatusInArrears, func(_ *testing.T, p *domain.Policy) error { return p.MarkInArrears(dt.Now()) }},
		{"Reinstate", []domain.Status{domain.StatusInArrears}, domain.StatusActive, func(_ *testing.T, p *domain.Policy) error { return p.Reinstate(dt.Now()) }},
		{"Cancel", []domain.Status{domain.StatusActive, domain.StatusInArrears}, domain.StatusCancelled, func(t *testing.T, p *domain.Policy) error { return p.Cancel(cancellation(t), dt.Now()) }},
		{"Expire", []domain.Status{domain.StatusActive}, domain.StatusExpired, func(t *testing.T, p *domain.Policy) error { return p.Expire(dt.Date(t, "2027-01-01"), dt.Now()) }},
		{"MarkRenewed", []domain.Status{domain.StatusExpired}, domain.StatusRenewed, func(_ *testing.T, p *domain.Policy) error {
			return p.MarkRenewed(domain.PolicyID(uuid.New()), dt.Now())
		}},
		{"MarkNotRenewed", []domain.Status{domain.StatusExpired}, domain.StatusNotRenewed, func(_ *testing.T, p *domain.Policy) error { return p.MarkNotRenewed(dt.Now()) }},
	}
	for _, op := range ops {
		for _, from := range allStatuses() {
			t.Run(op.name+"/"+string(from), func(t *testing.T) {
				t.Parallel()
				p := dt.PolicyIn(t, from)
				err := op.run(t, p)
				if contains(op.from, from) {
					require.NoError(t, err)
					assert.Equal(t, op.to, p.Status())
					return
				}
				require.ErrorIs(t, err, domain.ErrInvalidPolicyTransition)
				assert.Equal(t, from, p.Status(), "no cambia")
				assert.Empty(t, p.PullEvents(), "sin eventos")
			})
		}
	}
}

func contains(list []domain.Status, s domain.Status) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestNewPolicyInvariants(t *testing.T) {
	t.Parallel()
	id := domain.PolicyID(uuid.Must(uuid.NewV7()))
	p, err := domain.NewPolicy(id, dt.ValidTerms(t), dt.Now())
	require.NoError(t, err)
	assert.Equal(t, domain.StatusInQuotation, p.Status())
	assert.NotEqual(t, uuid.Nil, p.HolderID())

	_, err = domain.NewPolicy(domain.PolicyID{}, dt.ValidTerms(t), dt.Now())
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	usd, err := kernel.ParseMoney("10", kernel.USD)
	require.NoError(t, err)
	holder, other := uuid.New(), uuid.New()
	cases := []struct {
		name   string
		change func(*domain.Terms)
		err    error
	}{
		{"sin aseguradora", func(x *domain.Terms) { x.InsurerID = uuid.Nil }, domain.ErrInvalidReference},
		{"sin ramo", func(x *domain.Terms) { x.LineOfBusinessID = uuid.Nil }, domain.ErrInvalidReference},
		{"prima negativa", func(x *domain.Terms) { x.NetPremium = dt.Money(t, "-1") }, domain.ErrInvalidPremium},
		{"impuestos negativos", func(x *domain.Terms) { x.Taxes = dt.Money(t, "-1") }, domain.ErrInvalidPremium},
		{"monedas distintas", func(x *domain.Terms) { x.Taxes = usd }, domain.ErrInvalidPremium},
		{"el total excede numeric(19,4)", func(x *domain.Terms) {
			x.NetPremium, x.Taxes = dt.Money(t, "999999999999999"), dt.Money(t, "999999999999999")
		}, domain.ErrInvalidPremium},
		{"forma de pago", func(x *domain.Terms) { x.PaymentForm = "x" }, domain.ErrInvalidPaymentForm},
		{"sin tomador", func(x *domain.Terms) { x.Parties = []domain.PolicyParty{{PartyID: holder, Role: domain.RoleInsured}} }, domain.ErrInvalidHolder},
		{"dos tomadores", func(x *domain.Terms) {
			x.Parties = []domain.PolicyParty{{PartyID: holder, Role: domain.RoleHolder}, {PartyID: other, Role: domain.RoleHolder}, {PartyID: other, Role: domain.RoleInsured}}
		}, domain.ErrInvalidHolder},
		{"sin asegurado", func(x *domain.Terms) { x.Parties = []domain.PolicyParty{{PartyID: holder, Role: domain.RoleHolder}} }, domain.ErrInsuredRequired},
		{"parte repetida", func(x *domain.Terms) {
			x.Parties = []domain.PolicyParty{{PartyID: holder, Role: domain.RoleHolder}, {PartyID: other, Role: domain.RoleInsured}, {PartyID: other, Role: domain.RoleInsured}}
		}, domain.ErrInvalidParty},
		{"demasiadas partes", func(x *domain.Terms) {
			for range 400 {
				x.Parties = append(x.Parties, domain.PolicyParty{PartyID: uuid.New(), Role: domain.RoleBeneficiary})
			}
		}, domain.ErrTooManyParties},
		{"papel desconocido", func(x *domain.Terms) { x.Parties = append(x.Parties, domain.PolicyParty{PartyID: other, Role: "x"}) }, domain.ErrInvalidParty},
		{"cobertura sin nombre", func(x *domain.Terms) { x.Coverages[0].Name = " " }, domain.ErrInvalidCoverage},
		{"cobertura en otra moneda", func(x *domain.Terms) { x.Coverages[0].SumInsured = usd }, domain.ErrInvalidCoverage},
		{"suma negativa", func(x *domain.Terms) { x.Coverages[0].SumInsured = dt.Money(t, "-1") }, domain.ErrInvalidCoverage},
		{"bien sin descripción", func(x *domain.Terms) { x.Items[0].Description = "" }, domain.ErrInvalidItem},
		{"bien sin datos", func(x *domain.Terms) { x.Items[0].RiskData = nil }, domain.ErrInvalidItem},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			terms := dt.ValidTerms(t)
			c.change(&terms)
			_, err := domain.NewPolicy(domain.PolicyID(uuid.Must(uuid.NewV7())), terms, dt.Now())
			require.ErrorIs(t, err, c.err)
		})
	}
}

func TestUpdateTermsOnlyBeforeIssue(t *testing.T) {
	t.Parallel()
	for _, s := range allStatuses() {
		p := dt.PolicyIn(t, s)
		terms := dt.ValidTerms(t)
		terms.NetPremium = dt.Money(t, "2000000")
		err := p.UpdateTerms(terms, dt.Now())
		if s == domain.StatusInQuotation || s == domain.StatusInProcess {
			require.NoError(t, err, s)
			assert.True(t, p.Snapshot().Terms.NetPremium.Equal(dt.Money(t, "2000000")))
			continue
		}
		require.ErrorIs(t, err, domain.ErrNotEditable, s)
	}
	p := dt.PolicyIn(t, domain.StatusInProcess)
	bad := dt.ValidTerms(t)
	bad.PaymentForm = "x"
	require.ErrorIs(t, p.UpdateTerms(bad, dt.Now()), domain.ErrInvalidPaymentForm)
}

func TestIssueKycClearance(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		clearance domain.KycClearance
		err       error
	}{
		{"no exigido", domain.KycNotRequired, nil},
		{"aprobado", domain.KycApproved, nil},
		{"no aprobado", domain.KycNotApproved, domain.ErrKycNotApproved},
		{"valor cero: deny-by-default", domain.KycUndetermined, domain.ErrComplianceUnavailable},
		{"valor desconocido", domain.KycClearance(99), domain.ErrComplianceUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := dt.PolicyIn(t, domain.StatusInProcess)
			err := p.Issue("POL-1", c.clearance, dt.Now())
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
				assert.Equal(t, domain.StatusInProcess, p.Status())
				assert.Empty(t, p.PullEvents())
				return
			}
			require.NoError(t, err)
			s := p.Snapshot()
			assert.Equal(t, "POL-1", s.Number)
			require.NotNil(t, s.IssuedAt)
			events := p.PullEvents()
			require.Len(t, events, 1)
			assert.IsType(t, domain.PolicyIssued{}, events[0])
			assert.Empty(t, p.PullEvents(), "PullEvents vacía la lista")
		})
	}
}

func TestIssueValidatesNumber(t *testing.T) {
	t.Parallel()
	p := dt.PolicyIn(t, domain.StatusInProcess)
	require.ErrorIs(t, p.Issue("  ", domain.KycApproved, dt.Now()), domain.ErrInvalidNumber)
	require.ErrorIs(t, p.Issue(strings.Repeat("9", 61), domain.KycApproved, dt.Now()), domain.ErrInvalidNumber)
}

func TestRejectRequiresReason(t *testing.T) {
	t.Parallel()
	p := dt.PolicyIn(t, domain.StatusInProcess)
	require.ErrorIs(t, p.Reject("x", dt.Now()), domain.ErrInvalidReason)
	require.NoError(t, p.Reject("riesgo no asegurable", dt.Now()))
	assert.Equal(t, "riesgo no asegurable", p.Snapshot().RejectionReason)
}

// DECISIONS N7: el último día de vigencia está cubierto; vence al día siguiente.
func TestExpireBoundaries(t *testing.T) {
	t.Parallel()
	p := dt.PolicyIn(t, domain.StatusActive)
	require.ErrorIs(t, p.Expire(dt.Date(t, "2026-12-30"), dt.Now()), domain.ErrNotYetExpired)
	require.ErrorIs(t, p.Expire(dt.Date(t, "2026-12-31"), dt.Now()), domain.ErrNotYetExpired, "el último día sigue cubierto")
	require.NoError(t, p.Expire(dt.Date(t, "2027-01-01"), dt.Now()))
	events := p.PullEvents()
	require.Len(t, events, 1)
	assert.IsType(t, domain.PolicyExpired{}, events[0])
}

func TestArrearsEvents(t *testing.T) {
	t.Parallel()
	p := dt.PolicyIn(t, domain.StatusActive)
	require.NoError(t, p.MarkInArrears(dt.Now()))
	assert.IsType(t, domain.PolicyFellIntoArrears{}, p.PullEvents()[0])
	require.NoError(t, p.Reinstate(dt.Now()))
	assert.IsType(t, domain.PolicyReinstated{}, p.PullEvents()[0])
}

func TestRenewal(t *testing.T) {
	t.Parallel()
	expired := dt.PolicyIn(t, domain.StatusExpired)
	next := dt.PolicyIn(t, domain.StatusInQuotation)
	require.NoError(t, next.SetRenewedFrom(expired.ID()))
	assert.Equal(t, uuid.UUID(expired.ID()), next.Snapshot().RenewedFrom.UUID)
	before := expired.Snapshot().Terms.NetPremium
	require.NoError(t, expired.MarkRenewed(next.ID(), dt.Now()))
	assert.IsType(t, domain.PolicyRenewed{}, expired.PullEvents()[0])
	assert.True(t, before.Equal(expired.Snapshot().Terms.NetPremium), "la renovación no muta la póliza anterior")
	require.ErrorIs(t, dt.PolicyIn(t, domain.StatusActive).SetRenewedFrom(expired.ID()), domain.ErrNotEditable)
}

func TestCancellationRules(t *testing.T) {
	t.Parallel()
	usd, err := kernel.ParseMoney("1", kernel.USD)
	require.NoError(t, err)
	cases := []struct {
		name   string
		change func(*domain.Cancellation)
		err    error
	}{
		{"primer día", func(c *domain.Cancellation) { c.EffectiveDate = dt.Date(t, "2026-01-01") }, nil},
		{"último día (N7)", func(c *domain.Cancellation) { c.EffectiveDate = dt.Date(t, "2026-12-31") }, nil},
		{"sin prima no devengada", func(c *domain.Cancellation) { c.UnearnedPremium = dt.Money(t, "0") }, nil},
		{"toda la prima neta", func(c *domain.Cancellation) { c.UnearnedPremium = dt.Money(t, "1000000") }, nil},
		{"antes de la vigencia", func(c *domain.Cancellation) { c.EffectiveDate = dt.Date(t, "2025-12-31") }, domain.ErrCancellationDate},
		{"después de la vigencia", func(c *domain.Cancellation) { c.EffectiveDate = dt.Date(t, "2027-01-01") }, domain.ErrCancellationDate},
		{"más que la prima neta", func(c *domain.Cancellation) { c.UnearnedPremium = dt.Money(t, "1000000.01") }, domain.ErrInvalidUnearned},
		{"negativa", func(c *domain.Cancellation) { c.UnearnedPremium = dt.Money(t, "-1") }, domain.ErrInvalidUnearned},
		{"otra moneda", func(c *domain.Cancellation) { c.UnearnedPremium = usd }, domain.ErrInvalidUnearned},
		{"causa inválida", func(c *domain.Cancellation) { c.Cause = "x" }, domain.ErrInvalidCancellation},
		{"referencia larga", func(c *domain.Cancellation) { c.InsurerReference = strings.Repeat("x", 61) }, domain.ErrInvalidCancellation},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := dt.PolicyIn(t, domain.StatusInArrears)
			in := cancellation(t)
			c.change(&in)
			err := p.Cancel(in, dt.Now())
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
				assert.Equal(t, domain.StatusInArrears, p.Status())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, domain.StatusCancelled, p.Status())
			assert.IsType(t, domain.PolicyCancelled{}, p.PullEvents()[0])
		})
	}
}

func TestRequestEndorsementRules(t *testing.T) {
	t.Parallel()
	inside := dt.Date(t, "2026-06-01")
	for _, s := range allStatuses() {
		p := dt.PolicyIn(t, s)
		err := p.RequestEndorsement(uuid.New(), domain.EndorsementOther, "Cambio de dirección", inside, dt.Now())
		if s == domain.StatusActive || s == domain.StatusInArrears {
			require.NoError(t, err, s)
			assert.Len(t, p.Snapshot().Endorsements, 1)
			continue
		}
		require.ErrorIs(t, err, domain.ErrEndorsementNotAllowed, s)
	}
	p := dt.PolicyIn(t, domain.StatusActive)
	require.ErrorIs(t, p.RequestEndorsement(uuid.New(), domain.EndorsementOther, "Fuera", dt.Date(t, "2027-01-01"), dt.Now()), domain.ErrEndorsementDate)
	require.NoError(t, p.RequestEndorsement(uuid.New(), domain.EndorsementOther, "Último día", dt.Date(t, "2026-12-31"), dt.Now()), "N7")
	require.ErrorIs(t, p.RequestEndorsement(uuid.New(), "x", "Tipo", inside, dt.Now()), domain.ErrInvalidEndorsement)
	require.ErrorIs(t, p.RequestEndorsement(uuid.New(), domain.EndorsementOther, "x", inside, dt.Now()), domain.ErrInvalidEndorsement)
}

func requested(t *testing.T, status domain.Status) (*domain.Policy, uuid.UUID) {
	t.Helper()
	id := uuid.New()
	active := dt.PolicyIn(t, domain.StatusActive)
	require.NoError(t, active.RequestEndorsement(id, domain.EndorsementSumInsuredIncrease, "Aumento de suma", dt.Date(t, "2026-06-01"), dt.Now()))
	s := active.Snapshot()
	s.Status = status
	return domain.Rehydrate(s), id
}

func TestIssueEndorsementAppliesEffects(t *testing.T) {
	t.Parallel()
	p, id := requested(t, domain.StatusInArrears)
	before := p.Snapshot()
	assert.True(t, before.Terms.NetPremium.Equal(dt.Money(t, "1000000")), "solicitar no cambia la póliza")

	newParty := uuid.New()
	err := p.IssueEndorsement(id, "END-1", dt.Money(t, "-100000"), dt.Money(t, "-19000"), domain.EndorsementEffects{
		CoverageChanges: []domain.CoverageChange{{CoverageID: before.Terms.Coverages[0].ID, SumInsured: dt.Money(t, "60000000")}},
		AddParties:      []domain.PolicyParty{{PartyID: newParty, Role: domain.RoleBeneficiary}},
		ItemChanges:     []domain.ItemChange{{ItemID: before.Terms.Items[0].ID, RiskData: []byte(`{"plate":"XYZ987"}`)}},
	}, dt.Now())
	require.NoError(t, err)

	after := p.Snapshot()
	assert.True(t, after.Terms.NetPremium.Equal(dt.Money(t, "900000")))
	assert.True(t, after.Terms.Taxes.Equal(dt.Money(t, "171000")))
	assert.True(t, after.Terms.Coverages[0].SumInsured.Equal(dt.Money(t, "60000000")))
	assert.JSONEq(t, `{"plate":"XYZ987"}`, string(after.Terms.Items[0].RiskData))
	assert.Len(t, after.Terms.Parties, 3)
	assert.Equal(t, domain.EndorsementIssued, after.Endorsements[0].Status)
	assert.Equal(t, "END-1", after.Endorsements[0].Number)
	assert.True(t, before.Terms.Coverages[0].SumInsured.Equal(dt.Money(t, "50000000")), "la copia anterior no cambia")

	events := p.PullEvents()
	require.Len(t, events, 2, "el endoso y el cambio de partes")
	endorsed, ok := events[0].(domain.PolicyEndorsed)
	require.True(t, ok)
	assert.True(t, endorsed.NetPremiumDelta.Equal(dt.Money(t, "-100000")))
	changed, ok := events[1].(domain.PolicyPartiesChanged)
	require.True(t, ok)
	assert.Equal(t, before.Version+1, changed.Version)
	assert.Contains(t, changed.PartyIDs, newParty)

	require.ErrorIs(t, p.IssueEndorsement(id, "END-2", dt.Money(t, "0"), dt.Money(t, "0"), domain.EndorsementEffects{}, dt.Now()), domain.ErrEndorsementNotPending)
	require.ErrorIs(t, p.RejectEndorsement(id, "ya emitido", dt.Now()), domain.ErrEndorsementNotPending)
}

func TestIssueEndorsementIsAllOrNothing(t *testing.T) {
	t.Parallel()
	zero := func(t *testing.T) kernel.Money { t.Helper(); return dt.Money(t, "0") }
	usd, err := kernel.ParseMoney("1", kernel.USD)
	require.NoError(t, err)
	cases := []struct {
		name    string
		net     kernel.Money
		taxes   kernel.Money
		effects func(s domain.Snapshot) domain.EndorsementEffects
		err     error
	}{
		{"prima negativa", dt.Money(t, "-1000000.01"), zero(t), func(domain.Snapshot) domain.EndorsementEffects { return domain.EndorsementEffects{} }, domain.ErrEndorsementPremium},
		{"impuestos negativos", zero(t), dt.Money(t, "-190000.01"), func(domain.Snapshot) domain.EndorsementEffects { return domain.EndorsementEffects{} }, domain.ErrEndorsementPremium},
		{"moneda distinta", usd, zero(t), func(domain.Snapshot) domain.EndorsementEffects { return domain.EndorsementEffects{} }, domain.ErrEndorsementPremium},
		{"cobertura ajena", zero(t), zero(t), func(domain.Snapshot) domain.EndorsementEffects {
			return domain.EndorsementEffects{CoverageChanges: []domain.CoverageChange{{CoverageID: uuid.New(), SumInsured: dt.Money(t, "1")}}}
		}, domain.ErrEndorsementUnknownPart},
		{"bien ajeno", zero(t), zero(t), func(domain.Snapshot) domain.EndorsementEffects {
			return domain.EndorsementEffects{ItemChanges: []domain.ItemChange{{ItemID: uuid.New(), RiskData: []byte(`{}`)}}}
		}, domain.ErrEndorsementUnknownPart},
		{"parte ajena", zero(t), zero(t), func(domain.Snapshot) domain.EndorsementEffects {
			return domain.EndorsementEffects{RemoveParties: []domain.PolicyParty{{PartyID: uuid.New(), Role: domain.RoleInsured}}}
		}, domain.ErrEndorsementUnknownPart},
		{"queda sin asegurado", zero(t), zero(t), func(s domain.Snapshot) domain.EndorsementEffects {
			return domain.EndorsementEffects{RemoveParties: []domain.PolicyParty{{PartyID: s.Terms.Parties[1].PartyID, Role: domain.RoleInsured}}}
		}, domain.ErrInsuredRequired},
		{"queda sin tomador", zero(t), zero(t), func(s domain.Snapshot) domain.EndorsementEffects {
			return domain.EndorsementEffects{RemoveParties: []domain.PolicyParty{{PartyID: s.Terms.Parties[0].PartyID, Role: domain.RoleHolder}}}
		}, domain.ErrEndorsementHolderChange},
		{"cambio de tomador", zero(t), zero(t), func(s domain.Snapshot) domain.EndorsementEffects {
			return domain.EndorsementEffects{
				RemoveParties: []domain.PolicyParty{{PartyID: s.Terms.Parties[0].PartyID, Role: domain.RoleHolder}},
				AddParties:    []domain.PolicyParty{{PartyID: uuid.New(), Role: domain.RoleHolder}},
			}
		}, domain.ErrEndorsementHolderChange},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p, id := requested(t, domain.StatusActive)
			before := p.Snapshot()
			err := p.IssueEndorsement(id, "END-1", c.net, c.taxes, c.effects(before), dt.Now())
			require.ErrorIs(t, err, c.err)
			after := p.Snapshot()
			assert.True(t, before.Terms.NetPremium.Equal(after.Terms.NetPremium))
			assert.Equal(t, before.Terms.Parties, after.Terms.Parties)
			assert.Equal(t, domain.EndorsementRequested, after.Endorsements[0].Status)
			assert.Empty(t, p.PullEvents())
		})
	}
}

func TestEndorsementResolutionGates(t *testing.T) {
	t.Parallel()
	zero := dt.Money(t, "0")
	p, id := requested(t, domain.StatusCancelled)
	require.ErrorIs(t, p.IssueEndorsement(id, "END-1", zero, zero, domain.EndorsementEffects{}, dt.Now()), domain.ErrEndorsementNotAllowed, "cancelada: un endoso pendiente ya no se emite")
	require.NoError(t, p.RejectEndorsement(id, "póliza cancelada", dt.Now()), "pero sí se puede cerrar")

	active, id2 := requested(t, domain.StatusActive)
	require.ErrorIs(t, active.IssueEndorsement(uuid.New(), "END-1", zero, zero, domain.EndorsementEffects{}, dt.Now()), domain.ErrEndorsementNotFound)
	require.ErrorIs(t, active.IssueEndorsement(id2, " ", zero, zero, domain.EndorsementEffects{}, dt.Now()), domain.ErrInvalidEndorsement)
	require.ErrorIs(t, active.RejectEndorsement(id2, "x", dt.Now()), domain.ErrInvalidReason)
	require.ErrorIs(t, active.RejectEndorsement(uuid.New(), "motivo", dt.Now()), domain.ErrEndorsementNotFound)
	require.NoError(t, active.RejectEndorsement(id2, "la aseguradora no lo emite", dt.Now()))
	s := active.Snapshot()
	assert.Equal(t, domain.EndorsementRejected, s.Endorsements[0].Status)
	assert.True(t, s.Terms.NetPremium.Equal(dt.Money(t, "1000000")), "rechazar no cambia la póliza")
}

func TestPartiesChangedEvents(t *testing.T) {
	t.Parallel()
	terms := dt.ValidTerms(t)
	agent := uuid.New()
	terms.AgentID = uuid.NullUUID{UUID: agent, Valid: true}
	p, err := domain.NewPolicy(domain.PolicyID(uuid.Must(uuid.NewV7())), terms, dt.Now())
	require.NoError(t, err)
	events := p.PullEvents()
	require.Len(t, events, 2, "partes y asesor inicial")
	assert.IsType(t, domain.PolicyAgentChanged{}, events[1])
	created, ok := events[0].(domain.PolicyPartiesChanged)
	require.True(t, ok)
	assert.Equal(t, int32(1), created.Version)
	assert.Equal(t, agent, created.AgentID.UUID)
	assert.Len(t, created.PartyIDs, 1, "tomador y asegurado son el mismo tercero: sin repetidos")

	require.NoError(t, p.UpdateTerms(dt.ValidTerms(t), dt.Now()))
	updated, ok := p.PullEvents()[0].(domain.PolicyPartiesChanged)
	require.True(t, ok)
	assert.Equal(t, int32(2), updated.Version)
	assert.False(t, updated.AgentID.Valid)

	sameParties, id := requested(t, domain.StatusActive)
	require.NoError(t, sameParties.IssueEndorsement(id, "END-1", dt.Money(t, "0"), dt.Money(t, "0"), domain.EndorsementEffects{}, dt.Now()))
	assert.Len(t, sameParties.PullEvents(), 1, "sin cambio de partes no hay evento de partes")
}

func TestTerminalTransitionsStopGrantingAccess(t *testing.T) {
	t.Parallel()
	grants := func(t *testing.T, events []domain.Event) bool {
		t.Helper()
		for _, e := range events {
			if c, ok := e.(domain.PolicyPartiesChanged); ok {
				return c.GrantsAccess
			}
		}
		t.Fatal("sin PolicyPartiesChanged")
		return false
	}
	cases := []struct {
		name string
		from domain.Status
		run  func(*domain.Policy) error
	}{
		{"rechazada", domain.StatusInProcess, func(p *domain.Policy) error { return p.Reject("sin capacidad", dt.Now()) }},
		{"cancelada", domain.StatusActive, func(p *domain.Policy) error { return p.Cancel(cancellation(t), dt.Now()) }},
		{"renovada", domain.StatusExpired, func(p *domain.Policy) error { return p.MarkRenewed(domain.PolicyID(uuid.New()), dt.Now()) }},
		{"no renovada", domain.StatusExpired, func(p *domain.Policy) error { return p.MarkNotRenewed(dt.Now()) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := dt.PolicyIn(t, c.from)
			require.NoError(t, c.run(p))
			assert.False(t, grants(t, p.PullEvents()))
		})
	}
	// Vencida sigue dando acceso: es cuando se gestiona la renovación.
	expired := dt.PolicyIn(t, domain.StatusActive)
	require.NoError(t, expired.Expire(dt.Date(t, "2027-01-01"), dt.Now()))
	require.NoError(t, expired.ReassignAgent(uuid.New(), "Retiro del asesor", dt.Now()))
	assert.True(t, grants(t, expired.PullEvents()))
}

func TestReassignAgent(t *testing.T) {
	t.Parallel()
	agent := uuid.New()
	for _, s := range allStatuses() {
		p := dt.PolicyIn(t, s)
		err := p.ReassignAgent(agent, "Retiro del asesor", dt.Now())
		switch s {
		case domain.StatusRejected, domain.StatusCancelled, domain.StatusRenewed, domain.StatusNotRenewed:
			require.ErrorIs(t, err, domain.ErrAgentNotReassignable, s)
			assert.False(t, p.Snapshot().Terms.AgentID.Valid, "conserva su asesor como historial")
		case domain.StatusInQuotation, domain.StatusInProcess, domain.StatusActive, domain.StatusInArrears, domain.StatusExpired:
			require.NoError(t, err, s)
			assert.Equal(t, agent, p.Snapshot().Terms.AgentID.UUID)
			assert.Equal(t, s, p.Status(), "no cambia de estado")
		}
	}
	p := dt.PolicyIn(t, domain.StatusActive)
	first, second := uuid.New(), uuid.New()
	require.NoError(t, p.ReassignAgent(first, "Asignación", dt.Now()))
	p.PullEvents()
	require.ErrorIs(t, p.ReassignAgent(first, "Otra vez", dt.Now()), domain.ErrSameAgent)
	require.ErrorIs(t, p.ReassignAgent(second, "x", dt.Now()), domain.ErrInvalidTransferReason)
	require.ErrorIs(t, p.ReassignAgent(second, strings.Repeat("ñ", 101), dt.Now()), domain.ErrInvalidTransferReason, "límite en bytes de la bitácora")
	require.NoError(t, p.ReassignAgent(second, "Retiro del asesor", dt.Now()))
	var changed domain.PolicyAgentChanged
	for _, e := range p.PullEvents() {
		if c, ok := e.(domain.PolicyAgentChanged); ok {
			changed = c
		}
	}
	assert.Equal(t, first, changed.Previous.UUID)
	assert.Equal(t, second, changed.New.UUID)
	assert.Equal(t, "Retiro del asesor", changed.Reason)
}
