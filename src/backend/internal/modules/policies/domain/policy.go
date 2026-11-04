// Package domain contiene el agregado Policy (DOMAIN.md §2): máquina de estados, coberturas, bienes
// asegurados, partes y endosos. La agencia registra lo que expide la aseguradora; no calcula primas.
package domain

import (
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de dominio de policies (códigos estables).
var (
	ErrNotFound                = kernel.NewError(kernel.KindNotFound, "policies.policy.not_found", "póliza inexistente")
	ErrConcurrentUpdate        = kernel.NewError(kernel.KindConflict, "policies.policy.concurrent_update", "la póliza cambió; recargue y reintente")
	ErrInvalidPolicyTransition = kernel.NewError(kernel.KindConflict, "policies.policy.invalid_transition", "transición de estado inválida")
	ErrNotEditable             = kernel.NewError(kernel.KindConflict, "policies.policy.not_editable", "después de expedida, la póliza solo cambia por endoso")
	ErrNumberTaken             = kernel.NewError(kernel.KindConflict, "policies.policy.number_taken", "ese número de póliza ya existe en la aseguradora")
	ErrInvalidNumber           = kernel.NewError(kernel.KindValidation, "policies.policy.invalid_number", "número de póliza de 1 a 60 caracteres")
	ErrInvalidPremium          = kernel.NewError(kernel.KindValidation, "policies.policy.invalid_premium", "prima e impuestos no negativos y en la misma moneda")
	ErrInvalidPaymentForm      = kernel.NewError(kernel.KindValidation, "policies.policy.invalid_payment_form", "forma de pago desconocida")
	ErrInvalidReference        = kernel.NewError(kernel.KindValidation, "policies.policy.invalid_reference", "referencia inválida")
	ErrInvalidHolder           = kernel.NewError(kernel.KindValidation, "policies.policy.invalid_holder", "debe tener exactamente un tomador")
	ErrInsuredRequired         = kernel.NewError(kernel.KindValidation, "policies.policy.insured_required", "debe tener al menos un asegurado")
	ErrInvalidParty            = kernel.NewError(kernel.KindValidation, "policies.policy.invalid_party", "parte inválida o repetida")
	ErrInvalidCoverage         = kernel.NewError(kernel.KindValidation, "policies.policy.invalid_coverage", "cobertura inválida")
	ErrInvalidItem             = kernel.NewError(kernel.KindValidation, "policies.policy.invalid_item", "bien asegurado inválido")
	ErrInvalidReason           = kernel.NewError(kernel.KindValidation, "policies.policy.invalid_reason", "motivo de 3 a 500 caracteres")

	ErrKycNotApproved          = kernel.NewError(kernel.KindConflict, "policies.issue.kyc_not_approved", "el KYC del tomador no está aprobado")
	ErrComplianceUnavailable   = kernel.NewError(kernel.KindUnavailable, "policies.issue.compliance_unavailable", "no se pudo determinar si se exige KYC")
	ErrNotYetExpired           = kernel.NewError(kernel.KindConflict, "policies.policy.not_yet_expired", "la vigencia aún no termina")
	ErrInvalidCancellation     = kernel.NewError(kernel.KindValidation, "policies.cancellation.invalid", "causa de cancelación o referencia inválida")
	ErrCancellationDate        = kernel.NewError(kernel.KindValidation, "policies.cancellation.outside_coverage_period", "la fecha efectiva está fuera de la vigencia")
	ErrInvalidUnearned         = kernel.NewError(kernel.KindValidation, "policies.cancellation.invalid_unearned_premium", "la prima no devengada debe estar entre 0 y la prima neta")
	ErrEndorsementNotAllowed   = kernel.NewError(kernel.KindConflict, "policies.endorsement.policy_not_active", "los endosos aplican a pólizas vigentes o en mora")
	ErrEndorsementDate         = kernel.NewError(kernel.KindValidation, "policies.endorsement.outside_coverage_period", "la fecha efectiva está fuera de la vigencia")
	ErrInvalidEndorsement      = kernel.NewError(kernel.KindValidation, "policies.endorsement.invalid", "tipo, descripción o número de endoso inválido")
	ErrEndorsementNotFound     = kernel.NewError(kernel.KindNotFound, "policies.endorsement.not_found", "endoso inexistente")
	ErrEndorsementNotPending   = kernel.NewError(kernel.KindConflict, "policies.endorsement.not_pending", "el endoso ya fue resuelto")
	ErrEndorsementPremium      = kernel.NewError(kernel.KindValidation, "policies.endorsement.negative_premium", "la prima o los impuestos quedarían negativos")
	ErrEndorsementHolderChange = kernel.NewError(kernel.KindValidation, "policies.endorsement.holder_change_not_allowed", "un endoso no cambia el tomador: el nuevo tomador no pasaría el control de KYC")
	ErrEndorsementUnknownPart  = kernel.NewError(kernel.KindValidation, "policies.endorsement.unknown_target", "la cobertura, el bien o la parte no pertenece a la póliza")
)

// PolicyID identifica una póliza.
type PolicyID uuid.UUID

// String devuelve la forma canónica.
func (id PolicyID) String() string { return kernel.IDString(id) }

// Status es el estado de la póliza (DOMAIN.md §2).
type Status string

// Estados de la póliza.
const (
	StatusInQuotation Status = "in_quotation" // EnCotizacion
	StatusInProcess   Status = "in_process"   // EnTramite
	StatusActive      Status = "active"       // Vigente
	StatusInArrears   Status = "in_arrears"   // EnMora
	StatusCancelled   Status = "cancelled"    // Cancelada
	StatusExpired     Status = "expired"      // Vencida
	StatusRenewed     Status = "renewed"      // Renovada
	StatusNotRenewed  Status = "not_renewed"  // NoRenovada
	StatusRejected    Status = "rejected"     // Rechazada
)

// transitions es la tabla explícita de DOMAIN.md §2; cualquier otra transición es inválida.
func transitions() map[Status][]Status {
	return map[Status][]Status{
		StatusInQuotation: {StatusInProcess},
		StatusInProcess:   {StatusActive, StatusRejected},
		StatusActive:      {StatusInArrears, StatusCancelled, StatusExpired},
		StatusInArrears:   {StatusActive, StatusCancelled},
		StatusExpired:     {StatusRenewed, StatusNotRenewed},
	}
}

// PaymentForm es la forma de pago pactada (flujos.md Flujo 4).
type PaymentForm string

// Formas de pago.
const (
	PaymentSingle    PaymentForm = "single_payment"
	PaymentMonthly   PaymentForm = "monthly_installments"
	PaymentQuarterly PaymentForm = "quarterly_installments"
	PaymentAutoDebit PaymentForm = "automatic_debit"
	PaymentFinancing PaymentForm = "premium_financing"
)

const (
	maxReasonRunes    = 500
	maxShortTextRunes = 200
	maxReferenceRunes = 60
)

// PartyRole es el papel de un tercero en la póliza.
type PartyRole string

// Papeles.
const (
	RoleHolder      PartyRole = "holder"
	RoleInsured     PartyRole = "insured"
	RoleBeneficiary PartyRole = "beneficiary"
)

// PolicyParty vincula un tercero con la póliza.
type PolicyParty struct {
	PartyID uuid.UUID
	Role    PartyRole
}

// Coverage es un amparo de la póliza.
type Coverage struct {
	ID         uuid.UUID
	Name       string
	SumInsured kernel.Money
	Deductible string
}

// InsuredItem es un bien o persona asegurada. RiskData es JSON validado por el esquema del ramo
// (app, contra Catalog) y es dato Confidencial.
type InsuredItem struct {
	ID                uuid.UUID
	Description       string
	RiskSchemaKey     string
	RiskSchemaVersion int32
	RiskData          []byte
}

// Terms son los datos de la póliza que se pueden ajustar antes de expedirla.
type Terms struct {
	InsurerID        uuid.UUID
	LineOfBusinessID uuid.UUID
	ProductID        uuid.NullUUID
	AgentID          uuid.NullUUID
	NetPremium       kernel.Money
	Taxes            kernel.Money
	Period           kernel.DateRange
	PaymentForm      PaymentForm
	Parties          []PolicyParty
	Coverages        []Coverage
	Items            []InsuredItem
}

func (t Terms) validate() error {
	if t.InsurerID == uuid.Nil || t.LineOfBusinessID == uuid.Nil {
		return ErrInvalidReference
	}
	if err := validatePremium(t.NetPremium, t.Taxes); err != nil {
		return err
	}
	switch t.PaymentForm {
	case PaymentSingle, PaymentMonthly, PaymentQuarterly, PaymentAutoDebit, PaymentFinancing:
	default:
		return ErrInvalidPaymentForm
	}
	if err := validateParties(t.Parties); err != nil {
		return err
	}
	for _, c := range t.Coverages {
		n := utf8.RuneCountInString(strings.TrimSpace(c.Name))
		if c.ID == uuid.Nil || n < 2 || n > maxShortTextRunes || c.SumInsured.IsNegative() ||
			c.SumInsured.Currency() != t.NetPremium.Currency() || utf8.RuneCountInString(c.Deductible) > maxShortTextRunes {
			return ErrInvalidCoverage
		}
	}
	for _, i := range t.Items {
		n := utf8.RuneCountInString(strings.TrimSpace(i.Description))
		if i.ID == uuid.Nil || n < 2 || n > maxShortTextRunes || i.RiskSchemaKey == "" || i.RiskSchemaVersion < 1 || len(i.RiskData) == 0 {
			return ErrInvalidItem
		}
	}
	return nil
}

func validatePremium(net, taxes kernel.Money) error {
	if net.Currency() == "" || net.Currency() != taxes.Currency() || net.IsNegative() || taxes.IsNegative() {
		return ErrInvalidPremium
	}
	// El total también debe caber en numeric(19,4): se calcula en cada lectura.
	if _, err := net.Add(taxes); err != nil {
		return ErrInvalidPremium
	}
	return nil
}

// validateParties exige exactamente un tomador y al menos un asegurado, sin repetidos.
func validateParties(parties []PolicyParty) error {
	holders, insureds := 0, 0
	seen := make(map[PolicyParty]bool, len(parties))
	for _, p := range parties {
		if seen[p] || p.PartyID == uuid.Nil {
			return ErrInvalidParty
		}
		seen[p] = true
		switch p.Role {
		case RoleHolder:
			holders++
		case RoleInsured:
			insureds++
		case RoleBeneficiary:
		default:
			return ErrInvalidParty
		}
	}
	if holders != 1 {
		return ErrInvalidHolder
	}
	if insureds == 0 {
		return ErrInsuredRequired
	}
	return nil
}

// KycClearance es lo que el caso de uso decidió sobre el KYC del tomador al expedir. El valor cero
// se rechaza (deny-by-default, ADR 0012); la evaluación del flag ocurre fuera del agregado.
type KycClearance int

// Decisiones de KYC.
const (
	KycUndetermined KycClearance = iota
	KycNotRequired               // module.compliance apagado
	KycApproved                  // encendido, con KycCheck aprobado
	KycNotApproved               // encendido, sin KycCheck aprobado
)

// CancellationCause es la causa de una cancelación (flujos.md Flujo 8).
type CancellationCause string

// Causas de cancelación.
const (
	CauseClientRequest     CancellationCause = "client_request"
	CauseItemSold          CancellationCause = "item_sold"
	CauseArrears           CancellationCause = "arrears"
	CauseInsurerRevocation CancellationCause = "insurer_revocation"
	CauseOther             CancellationCause = "other"
)

// Cancellation es lo que expidió la aseguradora al cancelar.
type Cancellation struct {
	Cause            CancellationCause
	EffectiveDate    kernel.Date
	UnearnedPremium  kernel.Money
	InsurerReference string
}

// Policy es la raíz del agregado.
type Policy struct {
	id              PolicyID
	status          Status
	number          string
	terms           Terms
	renewedFrom     uuid.NullUUID
	issuedAt        *time.Time
	rejectionReason string
	cancellation    *Cancellation
	endorsements    []Endorsement
	createdAt       time.Time
	updatedAt       time.Time
	version         int32
	events          []Event
}

// NewPolicy crea una póliza en cotización validando sus invariantes.
func NewPolicy(id PolicyID, terms Terms, now time.Time) (*Policy, error) {
	if kernel.IsZeroID(id) {
		return nil, kernel.ErrInvalidID
	}
	if err := terms.validate(); err != nil {
		return nil, err
	}
	return &Policy{id: id, status: StatusInQuotation, terms: terms, createdAt: now.UTC(), updatedAt: now.UTC(), version: 1}, nil
}

// Snapshot es el estado persistido.
type Snapshot struct {
	ID              PolicyID
	Status          Status
	Number          string
	Terms           Terms
	RenewedFrom     uuid.NullUUID
	IssuedAt        *time.Time
	RejectionReason string
	Cancellation    *Cancellation
	Endorsements    []Endorsement
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Version         int32
}

// TotalPremium devuelve prima neta + impuestos (DECISIONS N8). No falla porque validatePremium
// exige que el total quepa, salvo datos guardados fuera del dominio.
func (s Snapshot) TotalPremium() (kernel.Money, error) {
	total, err := s.Terms.NetPremium.Add(s.Terms.Taxes)
	if err != nil {
		return kernel.Money{}, ErrInvalidPremium
	}
	return total, nil
}

// Rehydrate reconstruye desde la BD sin validaciones de creación.
func Rehydrate(s Snapshot) *Policy {
	return &Policy{
		id: s.ID, status: s.Status, number: s.Number, terms: s.Terms, renewedFrom: s.RenewedFrom, issuedAt: s.IssuedAt,
		rejectionReason: s.RejectionReason, cancellation: s.Cancellation, endorsements: s.Endorsements,
		createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, version: s.Version,
	}
}

// Snapshot devuelve una copia del estado.
func (p *Policy) Snapshot() Snapshot {
	terms := p.terms
	terms.Parties = slices.Clone(p.terms.Parties)
	terms.Coverages = slices.Clone(p.terms.Coverages)
	terms.Items = slices.Clone(p.terms.Items)
	return Snapshot{
		ID: p.id, Status: p.status, Number: p.number, Terms: terms, RenewedFrom: p.renewedFrom, IssuedAt: p.issuedAt,
		RejectionReason: p.rejectionReason, Cancellation: p.cancellation, Endorsements: slices.Clone(p.endorsements),
		CreatedAt: p.createdAt, UpdatedAt: p.updatedAt, Version: p.version,
	}
}

// ID devuelve el identificador.
func (p *Policy) ID() PolicyID { return p.id }

// Status devuelve el estado.
func (p *Policy) Status() Status { return p.status }

// Version devuelve la versión para concurrencia optimista.
func (p *Policy) Version() int32 { return p.version }

// HolderID devuelve el tercero tomador.
func (p *Policy) HolderID() uuid.UUID {
	for _, party := range p.terms.Parties {
		if party.Role == RoleHolder {
			return party.PartyID
		}
	}
	return uuid.Nil
}

// LogValue evita que el agregado (datos Confidenciales) llegue a un log.
func (p *Policy) LogValue() slog.Value { return slog.StringValue(p.id.String()) }

// PullEvents devuelve y vacía los eventos acumulados.
func (p *Policy) PullEvents() []Event {
	out := p.events
	p.events = nil
	return out
}

func (p *Policy) transitionTo(to Status, now time.Time) error {
	if !slices.Contains(transitions()[p.status], to) {
		return ErrInvalidPolicyTransition.WithMessage("%s → %s", p.status, to)
	}
	p.status = to
	p.updatedAt = now.UTC()
	return nil
}

// UpdateTerms ajusta los datos antes de expedir (Flujo 3: "con condiciones, informar y ajustar").
func (p *Policy) UpdateTerms(terms Terms, now time.Time) error {
	if p.status != StatusInQuotation && p.status != StatusInProcess {
		return ErrNotEditable
	}
	if err := terms.validate(); err != nil {
		return err
	}
	p.terms = terms
	p.updatedAt = now.UTC()
	return nil
}

// Accept registra que el cliente aceptó: EnCotizacion → EnTramite.
func (p *Policy) Accept(now time.Time) error { return p.transitionTo(StatusInProcess, now) }

// Issue registra la expedición de la aseguradora: EnTramite → Vigente. Exige el número asignado y
// un KYC aprobado o no exigido (DECISIONS N4).
func (p *Policy) Issue(number string, clearance KycClearance, now time.Time) error {
	if p.status != StatusInProcess {
		return ErrInvalidPolicyTransition.WithMessage("%s → %s", p.status, StatusActive)
	}
	number = strings.TrimSpace(number)
	if number == "" || utf8.RuneCountInString(number) > maxReferenceRunes {
		return ErrInvalidNumber
	}
	switch clearance {
	case KycNotRequired, KycApproved:
	case KycNotApproved:
		return ErrKycNotApproved
	case KycUndetermined:
		return ErrComplianceUnavailable
	default:
		return ErrComplianceUnavailable
	}
	if err := p.transitionTo(StatusActive, now); err != nil {
		return err
	}
	at := now.UTC()
	p.number, p.issuedAt = number, &at
	p.events = append(p.events, PolicyIssued{Policy: p.Snapshot()})
	return nil
}

// Reject registra el rechazo de la aseguradora: EnTramite → Rechazada.
func (p *Policy) Reject(reason string, now time.Time) error {
	reason, err := validReason(reason)
	if err != nil {
		return err
	}
	if err := p.transitionTo(StatusRejected, now); err != nil {
		return err
	}
	p.rejectionReason = reason
	return nil
}

func validReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 3 || n > maxReasonRunes {
		return "", ErrInvalidReason
	}
	return reason, nil
}

// MarkInArrears aplica cuando una cuota queda vencida: Vigente → EnMora.
func (p *Policy) MarkInArrears(now time.Time) error {
	if err := p.transitionTo(StatusInArrears, now); err != nil {
		return err
	}
	p.events = append(p.events, PolicyFellIntoArrears{PolicyID: p.id})
	return nil
}

// Reinstate aplica cuando se pone al día: EnMora → Vigente.
func (p *Policy) Reinstate(now time.Time) error {
	if p.status != StatusInArrears {
		return ErrInvalidPolicyTransition.WithMessage("%s → %s", p.status, StatusActive)
	}
	if err := p.transitionTo(StatusActive, now); err != nil {
		return err
	}
	p.events = append(p.events, PolicyReinstated{PolicyID: p.id})
	return nil
}

// Cancel registra la cancelación que expidió la aseguradora: Vigente o EnMora → Cancelada. La prima
// no devengada es el valor informado por la aseguradora (DECISIONS T7).
func (p *Policy) Cancel(c Cancellation, now time.Time) error {
	if !slices.Contains(transitions()[p.status], StatusCancelled) {
		return ErrInvalidPolicyTransition.WithMessage("%s → %s", p.status, StatusCancelled)
	}
	switch c.Cause {
	case CauseClientRequest, CauseItemSold, CauseArrears, CauseInsurerRevocation, CauseOther:
	default:
		return ErrInvalidCancellation
	}
	c.InsurerReference = strings.TrimSpace(c.InsurerReference)
	if utf8.RuneCountInString(c.InsurerReference) > maxReferenceRunes {
		return ErrInvalidCancellation
	}
	if !p.terms.Period.Contains(c.EffectiveDate) {
		return ErrCancellationDate
	}
	if c.UnearnedPremium.IsNegative() || c.UnearnedPremium.Currency() != p.terms.NetPremium.Currency() {
		return ErrInvalidUnearned
	}
	cmp, err := c.UnearnedPremium.Cmp(p.terms.NetPremium)
	if err != nil || cmp > 0 {
		return ErrInvalidUnearned
	}
	if err := p.transitionTo(StatusCancelled, now); err != nil {
		return err
	}
	p.cancellation = &c
	p.events = append(p.events, PolicyCancelled{PolicyID: p.id, Cancellation: c})
	return nil
}

// Expire marca el fin de vigencia: Vigente → Vencida, desde el día siguiente al último día cubierto
// (DECISIONS N7).
func (p *Policy) Expire(today kernel.Date, now time.Time) error {
	if p.status != StatusActive {
		return ErrInvalidPolicyTransition.WithMessage("%s → %s", p.status, StatusExpired)
	}
	if !today.After(p.terms.Period.To()) {
		return ErrNotYetExpired
	}
	if err := p.transitionTo(StatusExpired, now); err != nil {
		return err
	}
	p.events = append(p.events, PolicyExpired{PolicyID: p.id})
	return nil
}

// MarkRenewed registra que la póliza vencida se renovó en renewedBy: Vencida → Renovada. La nueva
// vigencia es otra Policy; esta no cambia de datos (DOMAIN.md §2).
func (p *Policy) MarkRenewed(renewedBy PolicyID, now time.Time) error {
	if err := p.transitionTo(StatusRenewed, now); err != nil {
		return err
	}
	p.events = append(p.events, PolicyRenewed{PolicyID: p.id, RenewedBy: renewedBy})
	return nil
}

// MarkNotRenewed cierra una póliza vencida sin renovación: Vencida → NoRenovada.
func (p *Policy) MarkNotRenewed(now time.Time) error { return p.transitionTo(StatusNotRenewed, now) }

// SetRenewedFrom vincula la nueva vigencia con la póliza que renueva. Solo antes de expedirla.
func (p *Policy) SetRenewedFrom(previous PolicyID) error {
	if p.status != StatusInQuotation && p.status != StatusInProcess {
		return ErrNotEditable
	}
	p.renewedFrom = uuid.NullUUID{UUID: uuid.UUID(previous), Valid: true}
	return nil
}
