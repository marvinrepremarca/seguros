// Package domain contiene el caso de renovación de una póliza (Flujo 6).
package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de negocio.
var (
	ErrInvalidTransition = kernel.NewError(kernel.KindConflict, "renewals.case.invalid_transition", "el caso no admite ese cambio en su estado actual")
	ErrInvalidReason     = kernel.NewError(kernel.KindValidation, "renewals.case.invalid_reason", "el motivo es obligatorio")
	ErrInvalidAlertDays  = kernel.NewError(kernel.KindValidation, "renewals.settings.invalid_alert_days", "los días de alerta deben estar entre 1 y 365, sin repetirse")
	ErrNotFound          = kernel.NewError(kernel.KindNotFound, "renewals.case.not_found", "caso de renovación inexistente")
	ErrConcurrentUpdate  = kernel.NewError(kernel.KindConflict, "renewals.case.concurrent_update", "el caso cambió; vuelva a cargarlo")
)

// Status es el estado de un caso.
type Status string

// Estados del caso.
const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusRenewed    Status = "renewed"
	StatusLost       Status = "lost"
	StatusClosed     Status = "closed"
)

// Un caso perdido puede renovarse si el cliente vuelve. Renovado y cerrado son terminales.
var transitions = map[Status][]Status{ //nolint:gochecknoglobals // tabla inmutable de transiciones
	StatusOpen:       {StatusInProgress, StatusRenewed, StatusLost, StatusClosed},
	StatusInProgress: {StatusRenewed, StatusLost, StatusClosed},
	StatusLost:       {StatusRenewed, StatusClosed},
}

// Snapshot son los datos del caso.
type Snapshot struct {
	ID            uuid.UUID
	PolicyID      uuid.UUID
	PolicyNumber  string
	InsurerID     uuid.UUID
	HolderPartyID uuid.UUID
	EffectiveTo   kernel.Date
	Status        Status
	LostReason    string
	RenewedBy     uuid.NullUUID
	Version       int32
}

// Case es el agregado.
type Case struct{ s Snapshot }

// Rehydrate reconstruye el caso.
func Rehydrate(s Snapshot) *Case { return &Case{s: s} }

// Snapshot devuelve una copia.
func (c *Case) Snapshot() Snapshot { return c.s }

func (c *Case) transitionTo(to Status) error {
	if !slices.Contains(transitions[c.s.Status], to) {
		return ErrInvalidTransition
	}
	c.s.Status = to
	return nil
}

// Start marca que la agencia inició la gestión.
func (c *Case) Start() error { return c.transitionTo(StatusInProgress) }

// MarkLost registra que el cliente no renueva, con su motivo (flujos.md Flujo 6).
func (c *Case) MarkLost(reason string) error {
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 3 || n > 500 {
		return ErrInvalidReason
	}
	if err := c.transitionTo(StatusLost); err != nil {
		return err
	}
	c.s.LostReason = reason
	return nil
}

// MarkRenewed vincula la nueva vigencia expedida.
func (c *Case) MarkRenewed(by uuid.UUID) error {
	if err := c.transitionTo(StatusRenewed); err != nil {
		return err
	}
	c.s.RenewedBy = uuid.NullUUID{UUID: by, Valid: true}
	return nil
}

// Close termina el caso porque la póliza se canceló. Uno ya renovado no cambia.
func (c *Case) Close() {
	if slices.Contains(transitions[c.s.Status], StatusClosed) {
		c.s.Status = StatusClosed
	}
}

// DefaultAlertDays son los umbrales por defecto (DECISIONS T2).
func DefaultAlertDays() []int32 { return []int32{60, 30, 15} }

// ValidateAlertDays valida los umbrales.
func ValidateAlertDays(days []int32) error {
	seen := map[int32]bool{}
	for _, d := range days {
		if d < 1 || d > 365 || seen[d] {
			return ErrInvalidAlertDays
		}
		seen[d] = true
	}
	if len(days) == 0 {
		return ErrInvalidAlertDays
	}
	return nil
}

// DueAlerts devuelve los umbrales cruzados y aún no avisados, de mayor a menor.
func DueAlerts(thresholds, sent []int32, daysLeft int32) []int32 {
	var out []int32
	for _, t := range thresholds {
		if daysLeft <= t && !slices.Contains(sent, t) {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b int32) int { return int(b - a) })
	return out
}
