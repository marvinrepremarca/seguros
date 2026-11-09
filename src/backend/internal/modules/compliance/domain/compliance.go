// Package domain contiene KYC, PQR y solicitudes de supresión (Flujo 10).
package domain

import (
	"crypto/subtle"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de negocio.
var (
	ErrPartyNotFound     = kernel.NewError(kernel.KindNotFound, "compliance.party.not_found", "tercero inexistente")
	ErrNotFound          = kernel.NewError(kernel.KindNotFound, "compliance.record.not_found", "registro inexistente")
	ErrInvalidCheck      = kernel.NewError(kernel.KindValidation, "compliance.kyc.invalid", "los datos de la verificación no son válidos")
	ErrInvalidComplaint  = kernel.NewError(kernel.KindValidation, "compliance.complaint.invalid", "los datos de la queja no son válidos")
	ErrResolutionNeeded  = kernel.NewError(kernel.KindValidation, "compliance.resolution_required", "la respuesta es obligatoria")
	ErrInvalidTransition = kernel.NewError(kernel.KindConflict, "compliance.complaint.invalid_transition", "la queja no admite ese cambio")
	ErrAlreadyResolved   = kernel.NewError(kernel.KindConflict, "compliance.suppression.already_resolved", "la solicitud ya se resolvió")
	ErrConcurrentUpdate  = kernel.NewError(kernel.KindConflict, "compliance.concurrent_update", "el registro cambió; vuelva a cargarlo")
)

// Result es el resultado de una verificación.
type Result string

// Resultados.
const (
	ResultApproved Result = "approved"
	ResultRejected Result = "rejected"
)

// KycCheck es una verificación inmutable registrada por el oficial (DECISIONS N33).
type KycCheck struct {
	ID          uuid.UUID
	PartyID     uuid.UUID
	Fingerprint []byte
	Result      Result
	Source      string
	CheckedOn   kernel.Date
	HasDetails  bool
}

// NewKycCheck valida la verificación. La fecha no puede ser futura.
func NewKycCheck(c KycCheck, today kernel.Date) (KycCheck, error) {
	c.Source = strings.TrimSpace(c.Source)
	n := utf8.RuneCountInString(c.Source)
	if (c.Result != ResultApproved && c.Result != ResultRejected) || n < 2 || n > 200 || c.CheckedOn.IsZero() || c.CheckedOn.After(today) || len(c.Fingerprint) == 0 {
		return KycCheck{}, ErrInvalidCheck
	}
	return c, nil
}

// Approves indica si la verificación más reciente aprueba el documento vigente (ADR 0012): al rectificar
// el documento, la huella cambia y el KYC deja de valer.
func (c KycCheck) Approves(current []byte) bool {
	return c.Result == ResultApproved && len(current) > 0 && subtle.ConstantTimeCompare(c.Fingerprint, current) == 1
}

// ComplaintStatus es el estado de una queja.
type ComplaintStatus string

// Estados.
const (
	ComplaintReceived   ComplaintStatus = "received"
	ComplaintInProgress ComplaintStatus = "in_progress"
	ComplaintResolved   ComplaintStatus = "resolved"
)

var complaintTransitions = map[ComplaintStatus][]ComplaintStatus{ //nolint:gochecknoglobals // tabla inmutable
	ComplaintReceived:   {ComplaintInProgress, ComplaintResolved},
	ComplaintInProgress: {ComplaintResolved},
}

// ComplaintData son los datos de una queja.
type ComplaintData struct {
	ID          uuid.UUID
	PartyID     uuid.NullUUID
	Channel     string
	Subject     string
	Description string
	Status      ComplaintStatus
	Resolution  string
	ReceivedOn  kernel.Date
	Version     int32
}

// Complaint es el agregado de una PQR.
type Complaint struct{ s ComplaintData }

func runes(s string, lo, hi int) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= lo && n <= hi
}

// NewComplaint registra una queja recibida.
func NewComplaint(s ComplaintData, today kernel.Date) (*Complaint, error) {
	if !runes(s.Channel, 2, 60) || !runes(s.Subject, 3, 200) || !runes(s.Description, 3, 4000) || s.ReceivedOn.IsZero() || s.ReceivedOn.After(today) {
		return nil, ErrInvalidComplaint
	}
	s.Status, s.Version = ComplaintReceived, 1
	return &Complaint{s: s}, nil
}

// RehydrateComplaint reconstruye la queja.
func RehydrateComplaint(s ComplaintData) *Complaint { return &Complaint{s: s} }

// Data devuelve una copia.
func (c *Complaint) Data() ComplaintData { return c.s }

// Start pasa la queja a gestión.
func (c *Complaint) Start() error {
	if !slices.Contains(complaintTransitions[c.s.Status], ComplaintInProgress) {
		return ErrInvalidTransition
	}
	c.s.Status = ComplaintInProgress
	return nil
}

// Resolve cierra la queja con su respuesta.
func (c *Complaint) Resolve(resolution string) error {
	if !runes(resolution, 3, 4000) {
		return ErrResolutionNeeded
	}
	if !slices.Contains(complaintTransitions[c.s.Status], ComplaintResolved) {
		return ErrInvalidTransition
	}
	c.s.Status, c.s.Resolution = ComplaintResolved, strings.TrimSpace(resolution)
	return nil
}

// SuppressionStatus es el estado de una solicitud de supresión.
type SuppressionStatus string

// Estados de una solicitud de supresión: se decide una sola vez.
const (
	SuppressionRequested SuppressionStatus = "requested"
	SuppressionAccepted  SuppressionStatus = "accepted"
	SuppressionDenied    SuppressionStatus = "denied"
)

// ResolveSuppression decide una solicitud pendiente.
func ResolveSuppression(current SuppressionStatus, accepted bool) (SuppressionStatus, error) {
	if current != SuppressionRequested {
		return "", ErrAlreadyResolved
	}
	if accepted {
		return SuppressionAccepted, nil
	}
	return SuppressionDenied, nil
}

// ValidateResolution valida la respuesta a una solicitud de supresión.
func ValidateResolution(text string) error {
	if !runes(text, 3, 2000) {
		return ErrResolutionNeeded
	}
	return nil
}
