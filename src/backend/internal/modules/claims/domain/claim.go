// Package domain contiene el siniestro (Flujo 7). La agencia registra lo que decide la aseguradora.
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

// Errores de negocio.
var (
	ErrNotFound          = kernel.NewError(kernel.KindNotFound, "claims.claim.not_found", "siniestro inexistente")
	ErrPolicyNotFound    = kernel.NewError(kernel.KindNotFound, "claims.claim.policy_not_found", "póliza inexistente")
	ErrItemNotFound      = kernel.NewError(kernel.KindNotFound, "claims.checklist.not_found", "elemento inexistente")
	ErrNoHealthDetails   = kernel.NewError(kernel.KindNotFound, "claims.health.not_found", "el siniestro no tiene detalle de salud")
	ErrInvalidTransition = kernel.NewError(kernel.KindConflict, "claims.claim.invalid_transition", "el siniestro no admite ese cambio en su estado actual")
	ErrConcurrentUpdate  = kernel.NewError(kernel.KindConflict, "claims.claim.concurrent_update", "el siniestro cambió; vuelva a cargarlo")
	ErrInvalidData       = kernel.NewError(kernel.KindValidation, "claims.claim.invalid_data", "los datos del siniestro no son válidos")
	ErrFutureOccurrence  = kernel.NewError(kernel.KindValidation, "claims.claim.future_occurrence", "la fecha de ocurrencia no puede ser futura")
	ErrSupportRequired   = kernel.NewError(kernel.KindValidation, "claims.claim.support_required", "indique el soporte de la aseguradora")
	ErrInvalidAmount     = kernel.NewError(kernel.KindValidation, "claims.claim.invalid_amount", "el valor no es válido")
	ErrClaimedRequired   = kernel.NewError(kernel.KindConflict, "claims.claim.claimed_required", "registre primero el valor reclamado")
	ErrPaidExceedsClaim  = kernel.NewError(kernel.KindConflict, "claims.claim.paid_exceeds_claimed", "el pago supera lo reclamado: requiere aprobación de un gerente")
	ErrInvalidText       = kernel.NewError(kernel.KindValidation, "claims.claim.invalid_text", "el texto no es válido")
)

// Status es el estado del siniestro.
type Status string

// Estados.
const (
	StatusReported    Status = "reported"
	StatusNotified    Status = "notified"
	StatusDocumenting Status = "documenting"
	StatusEvaluating  Status = "evaluating"
	StatusApproved    Status = "approved"
	StatusObjected    Status = "objected"
	StatusPaid        Status = "paid"
	StatusClosed      Status = "closed"
)

var transitions = map[Status][]Status{ //nolint:gochecknoglobals // tabla inmutable (DOMAIN.md)
	StatusReported:    {StatusNotified},
	StatusNotified:    {StatusDocumenting},
	StatusDocumenting: {StatusEvaluating},
	StatusEvaluating:  {StatusApproved, StatusObjected},
	StatusApproved:    {StatusPaid},
	StatusObjected:    {StatusEvaluating, StatusClosed},
	StatusPaid:        {StatusClosed},
}

// Snapshot son los datos del siniestro. El detalle de salud nunca pasa por el agregado.
type Snapshot struct {
	ID                 uuid.UUID
	PolicyID           uuid.UUID
	PolicyNumber       string
	InsurerID          uuid.UUID
	HolderPartyID      uuid.UUID
	OccurredOn         kernel.Date
	Location           string
	ClaimType          string
	Description        string
	Currency           kernel.Currency
	ClaimedAmount      *kernel.Money
	PaidAmount         *kernel.Money
	Status             Status
	InsurerClaimNumber string
	DecisionSupport    string
	AlertOutOfCoverage bool
	AlertInArrears     bool
	AlertNotInForce    bool
	HasHealthDetails   bool
	ReportedAt         time.Time
	NotifiedAt         *time.Time
	Version            int32
}

// Claim es el agregado.
type Claim struct{ s Snapshot }

// PolicyView es lo que el siniestro necesita saber de la póliza.
type PolicyView struct {
	InForce, InArrears bool
	From, To           kernel.Date
}

func textOK(s string, minRunes, maxRunes int) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= minRunes && n <= maxRunes
}

// New registra un siniestro. Una póliza fuera de vigencia, sin vigor o en mora genera alerta, no bloqueo:
// la aseguradora decide (DOMAIN.md).
func New(s Snapshot, policy PolicyView, today kernel.Date, now time.Time) (*Claim, error) {
	if !textOK(s.Location, 2, 200) || !textOK(s.ClaimType, 2, 100) || !textOK(s.Description, 3, 2000) || s.OccurredOn.IsZero() {
		return nil, ErrInvalidData
	}
	if s.OccurredOn.After(today) {
		return nil, ErrFutureOccurrence
	}
	if s.ClaimedAmount != nil && (s.ClaimedAmount.IsNegative() || s.ClaimedAmount.Currency() != s.Currency) {
		return nil, ErrInvalidAmount
	}
	s.AlertOutOfCoverage = s.OccurredOn.Before(policy.From) || s.OccurredOn.After(policy.To)
	s.AlertInArrears, s.AlertNotInForce = policy.InArrears, !policy.InForce
	s.Status, s.ReportedAt, s.Version = StatusReported, now.UTC(), 1
	return &Claim{s: s}, nil
}

// Rehydrate reconstruye el siniestro.
func Rehydrate(s Snapshot) *Claim { return &Claim{s: s} }

// Snapshot devuelve una copia.
func (c *Claim) Snapshot() Snapshot { return c.s }

// LogValue evita que los datos del siniestro lleguen a los logs.
func (c *Claim) LogValue() slog.Value { return slog.StringValue(c.s.ID.String()) }

func (c *Claim) transitionTo(to Status) error {
	if !slices.Contains(transitions[c.s.Status], to) {
		return ErrInvalidTransition
	}
	c.s.Status = to
	return nil
}

// Notify registra el aviso formal a la aseguradora.
func (c *Claim) Notify(insurerNumber string, now time.Time) error {
	if utf8.RuneCountInString(insurerNumber) > 60 {
		return ErrInvalidText
	}
	if err := c.transitionTo(StatusNotified); err != nil {
		return err
	}
	at := now.UTC()
	c.s.InsurerClaimNumber, c.s.NotifiedAt = strings.TrimSpace(insurerNumber), &at
	return nil
}

// StartDocumentation pasa a recolectar soportes.
func (c *Claim) StartDocumentation() error { return c.transitionTo(StatusDocumenting) }

// StartEvaluation pasa a evaluación; desde objetado es una reconsideración.
func (c *Claim) StartEvaluation() error { return c.transitionTo(StatusEvaluating) }

func (c *Claim) decide(to Status, support string) error {
	if !textOK(support, 3, 200) {
		return ErrSupportRequired
	}
	if err := c.transitionTo(to); err != nil {
		return err
	}
	c.s.DecisionSupport = strings.TrimSpace(support)
	return nil
}

// Approve registra la aprobación de la aseguradora, con su soporte.
func (c *Claim) Approve(support string) error { return c.decide(StatusApproved, support) }

// Object registra la objeción de la aseguradora, con su soporte.
func (c *Claim) Object(support string) error { return c.decide(StatusObjected, support) }

// Close cierra el siniestro pagado u objetado.
func (c *Claim) Close() error { return c.transitionTo(StatusClosed) }

// SetClaimed fija el valor reclamado antes de la decisión.
func (c *Claim) SetClaimed(amount kernel.Money) error {
	switch c.s.Status {
	case StatusReported, StatusNotified, StatusDocumenting, StatusEvaluating:
	case StatusApproved, StatusObjected, StatusPaid, StatusClosed:
		return ErrInvalidTransition
	}
	if amount.IsNegative() || amount.Currency() != c.s.Currency {
		return ErrInvalidAmount
	}
	c.s.ClaimedAmount = &amount
	return nil
}

// RecordPayment registra el pago informado. Superar lo reclamado exige override (DECISIONS N30).
func (c *Claim) RecordPayment(amount kernel.Money, override bool) error {
	if !amount.Amount().IsPositive() || amount.Currency() != c.s.Currency {
		return ErrInvalidAmount
	}
	if c.s.Status != StatusApproved {
		return ErrInvalidTransition
	}
	if c.s.ClaimedAmount == nil {
		return ErrClaimedRequired
	}
	cmp, err := amount.Cmp(*c.s.ClaimedAmount)
	if err != nil {
		return ErrInvalidAmount
	}
	if cmp > 0 && !override {
		return ErrPaidExceedsClaim
	}
	if err := c.transitionTo(StatusPaid); err != nil {
		return err
	}
	c.s.PaidAmount = &amount
	return nil
}

// ValidateHealth valida el detalle de salud antes de cifrarlo.
func ValidateHealth(details string) error {
	if !textOK(details, 3, 4000) {
		return ErrInvalidText
	}
	return nil
}

// ValidateLabel valida un elemento de la lista de control.
func ValidateLabel(label string) error {
	if !textOK(label, 2, 200) {
		return ErrInvalidText
	}
	return nil
}

// ValidateNote valida una gestión.
func ValidateNote(body string) error {
	if !textOK(body, 3, 2000) {
		return ErrInvalidText
	}
	return nil
}
