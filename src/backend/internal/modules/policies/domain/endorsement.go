package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// EndorsementType es el tipo de endoso (flujos.md Flujo 5).
type EndorsementType string

// Tipos de endoso.
const (
	EndorsementDataChange         EndorsementType = "data_change"
	EndorsementItemChange         EndorsementType = "item_change"
	EndorsementPartyInclusion     EndorsementType = "party_inclusion"
	EndorsementPartyExclusion     EndorsementType = "party_exclusion"
	EndorsementSumInsuredIncrease EndorsementType = "sum_insured_increase"
	EndorsementSumInsuredDecrease EndorsementType = "sum_insured_decrease"
	EndorsementOther              EndorsementType = "other"
)

// EndorsementStatus es el estado de un endoso (DECISIONS N9).
type EndorsementStatus string

// Estados del endoso.
const (
	EndorsementRequested EndorsementStatus = "requested"
	EndorsementIssued    EndorsementStatus = "issued"
	EndorsementRejected  EndorsementStatus = "rejected"
)

// CoverageChange fija la nueva suma asegurada de una cobertura.
type CoverageChange struct {
	CoverageID uuid.UUID
	SumInsured kernel.Money
}

// ItemChange reemplaza los datos de riesgo de un bien asegurado (ya validados contra su esquema).
type ItemChange struct {
	ItemID   uuid.UUID
	RiskData []byte
}

// EndorsementEffects son los cambios sobre la póliza, además de la prima.
type EndorsementEffects struct {
	CoverageChanges []CoverageChange
	AddParties      []PolicyParty
	RemoveParties   []PolicyParty
	ItemChanges     []ItemChange
}

// Endorsement es una modificación de una póliza vigente. Solo cambia por los métodos de Policy.
type Endorsement struct {
	ID              uuid.UUID
	Type            EndorsementType
	Status          EndorsementStatus
	Description     string
	EffectiveDate   kernel.Date
	RequestedAt     time.Time
	ResolvedAt      *time.Time
	Number          string
	NetPremiumDelta *kernel.Money
	TaxesDelta      *kernel.Money
	Effects         EndorsementEffects
	RejectionReason string
}

func (p *Policy) canEndorse() bool { return p.status == StatusActive || p.status == StatusInArrears }

// RequestEndorsement registra la solicitud. La póliza no cambia hasta que el endoso se emita.
func (p *Policy) RequestEndorsement(id uuid.UUID, kind EndorsementType, description string, effective kernel.Date, now time.Time) error {
	if !p.canEndorse() {
		return ErrEndorsementNotAllowed
	}
	switch kind {
	case EndorsementDataChange, EndorsementItemChange, EndorsementPartyInclusion, EndorsementPartyExclusion,
		EndorsementSumInsuredIncrease, EndorsementSumInsuredDecrease, EndorsementOther:
	default:
		return ErrInvalidEndorsement
	}
	description = strings.TrimSpace(description)
	if n := utf8.RuneCountInString(description); id == uuid.Nil || n < 3 || n > maxReasonRunes {
		return ErrInvalidEndorsement
	}
	if !p.terms.Period.Contains(effective) {
		return ErrEndorsementDate
	}
	p.endorsements = append(p.endorsements, Endorsement{
		ID: id, Type: kind, Status: EndorsementRequested, Description: description, EffectiveDate: effective, RequestedAt: now.UTC(),
	})
	p.updatedAt = now.UTC()
	return nil
}

func (p *Policy) pendingEndorsement(id uuid.UUID) (int, error) {
	i := slices.IndexFunc(p.endorsements, func(e Endorsement) bool { return e.ID == id })
	if i < 0 {
		return 0, ErrEndorsementNotFound
	}
	if p.endorsements[i].Status != EndorsementRequested {
		return 0, ErrEndorsementNotPending
	}
	return i, nil
}

// IssueEndorsement registra el endoso emitido y aplica sus efectos de una sola vez: si alguno es
// inválido, la póliza no cambia.
func (p *Policy) IssueEndorsement(id uuid.UUID, number string, netDelta, taxesDelta kernel.Money, effects EndorsementEffects, now time.Time) error {
	i, err := p.pendingEndorsement(id)
	if err != nil {
		return err
	}
	if !p.canEndorse() {
		return ErrEndorsementNotAllowed
	}
	number = strings.TrimSpace(number)
	if number == "" || utf8.RuneCountInString(number) > maxReferenceRunes {
		return ErrInvalidEndorsement
	}
	terms, err := p.applyEffects(netDelta, taxesDelta, effects)
	if err != nil {
		return err
	}
	p.terms = terms
	at := now.UTC()
	e := &p.endorsements[i]
	e.Status, e.Number, e.NetPremiumDelta, e.TaxesDelta, e.Effects, e.ResolvedAt = EndorsementIssued, number, &netDelta, &taxesDelta, effects, &at
	p.updatedAt = at
	p.events = append(p.events, PolicyEndorsed{PolicyID: p.id, EndorsementID: id, NetPremiumDelta: netDelta, TaxesDelta: taxesDelta, EffectiveDate: e.EffectiveDate})
	return nil
}

func (p *Policy) applyEffects(netDelta, taxesDelta kernel.Money, effects EndorsementEffects) (Terms, error) {
	t := p.terms
	t.Coverages, t.Items, t.Parties = slices.Clone(t.Coverages), slices.Clone(t.Items), slices.Clone(t.Parties)
	net, err := t.NetPremium.Add(netDelta)
	if err != nil {
		return Terms{}, ErrEndorsementPremium
	}
	taxes, err := t.Taxes.Add(taxesDelta)
	if err != nil {
		return Terms{}, ErrEndorsementPremium
	}
	if net.IsNegative() || taxes.IsNegative() {
		return Terms{}, ErrEndorsementPremium
	}
	if err := validatePremium(net, taxes); err != nil {
		return Terms{}, ErrEndorsementPremium
	}
	t.NetPremium, t.Taxes = net, taxes
	// El tomador no cambia por endoso: el nuevo no pasaría el control de KYC de la expedición (N4).
	for _, party := range slices.Concat(effects.AddParties, effects.RemoveParties) {
		if party.Role == RoleHolder {
			return Terms{}, ErrEndorsementHolderChange
		}
	}
	for _, c := range effects.CoverageChanges {
		j := slices.IndexFunc(t.Coverages, func(x Coverage) bool { return x.ID == c.CoverageID })
		if j < 0 {
			return Terms{}, ErrEndorsementUnknownPart
		}
		if c.SumInsured.IsNegative() || c.SumInsured.Currency() != t.NetPremium.Currency() {
			return Terms{}, ErrInvalidCoverage
		}
		t.Coverages[j].SumInsured = c.SumInsured
	}
	for _, c := range effects.ItemChanges {
		j := slices.IndexFunc(t.Items, func(x InsuredItem) bool { return x.ID == c.ItemID })
		if j < 0 {
			return Terms{}, ErrEndorsementUnknownPart
		}
		if len(c.RiskData) == 0 {
			return Terms{}, ErrInvalidItem
		}
		t.Items[j].RiskData = c.RiskData
	}
	for _, r := range effects.RemoveParties {
		j := slices.Index(t.Parties, r)
		if j < 0 {
			return Terms{}, ErrEndorsementUnknownPart
		}
		t.Parties = slices.Delete(t.Parties, j, j+1)
	}
	t.Parties = append(t.Parties, effects.AddParties...)
	if err := validateParties(t.Parties); err != nil {
		return Terms{}, err
	}
	return t, nil
}

// RejectEndorsement registra que la aseguradora no emitió el endoso. La póliza no cambia.
func (p *Policy) RejectEndorsement(id uuid.UUID, reason string, now time.Time) error {
	i, err := p.pendingEndorsement(id)
	if err != nil {
		return err
	}
	reason, err = validReason(reason)
	if err != nil {
		return err
	}
	at := now.UTC()
	e := &p.endorsements[i]
	e.Status, e.RejectionReason, e.ResolvedAt = EndorsementRejected, reason, &at
	p.updatedAt = at
	return nil
}

// Event es un evento de dominio de la póliza.
type Event interface{ policyEvent() }

// PolicyIssued: la aseguradora expidió la póliza.
type PolicyIssued struct{ Policy Snapshot }

// PolicyEndorsed: se emitió un endoso.
type PolicyEndorsed struct {
	PolicyID        PolicyID
	EndorsementID   uuid.UUID
	NetPremiumDelta kernel.Money
	TaxesDelta      kernel.Money
	EffectiveDate   kernel.Date
}

// PolicyCancelled: la aseguradora canceló la póliza.
type PolicyCancelled struct {
	PolicyID     PolicyID
	Cancellation Cancellation
}

// PolicyExpired: terminó la vigencia.
type PolicyExpired struct{ PolicyID PolicyID }

// PolicyRenewed: la póliza vencida se renovó en otra.
type PolicyRenewed struct {
	PolicyID  PolicyID
	RenewedBy PolicyID
}

// PolicyFellIntoArrears: la póliza entró en mora.
type PolicyFellIntoArrears struct{ PolicyID PolicyID }

// PolicyReinstated: la póliza salió de mora.
type PolicyReinstated struct{ PolicyID PolicyID }

func (PolicyIssued) policyEvent()          {}
func (PolicyEndorsed) policyEvent()        {}
func (PolicyCancelled) policyEvent()       {}
func (PolicyExpired) policyEvent()         {}
func (PolicyRenewed) policyEvent()         {}
func (PolicyFellIntoArrears) policyEvent() {}
func (PolicyReinstated) policyEvent()      {}
