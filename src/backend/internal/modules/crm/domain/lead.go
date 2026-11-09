// Package domain contiene el prospecto y su embudo (Flujo 1).
package domain

import (
	"log/slog"
	"net/mail"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de negocio.
var (
	ErrNotFound          = kernel.NewError(kernel.KindNotFound, "crm.lead.not_found", "prospecto inexistente")
	ErrInvalidTransition = kernel.NewError(kernel.KindConflict, "crm.lead.invalid_transition", "el prospecto no admite ese cambio en su estado actual")
	ErrConcurrentUpdate  = kernel.NewError(kernel.KindConflict, "crm.lead.concurrent_update", "el prospecto cambió; vuelva a cargarlo")
	ErrInvalidName       = kernel.NewError(kernel.KindValidation, "crm.lead.invalid_name", "el nombre debe tener entre 2 y 200 caracteres")
	ErrContactRequired   = kernel.NewError(kernel.KindValidation, "crm.lead.contact_required", "indique un correo o un teléfono válidos")
	ErrInvalidSource     = kernel.NewError(kernel.KindValidation, "crm.lead.invalid_source", "origen desconocido")
	ErrInvalidReason     = kernel.NewError(kernel.KindValidation, "crm.lead.invalid_reason", "el motivo es obligatorio")
	ErrInvalidFollowUp   = kernel.NewError(kernel.KindValidation, "crm.lead.invalid_follow_up", "la fecha de seguimiento no puede ser pasada")
	ErrInvalidParty      = kernel.NewError(kernel.KindValidation, "crm.lead.invalid_party", "el cliente no existe o está fuera de su alcance")
	ErrInvalidAgent      = kernel.NewError(kernel.KindValidation, "crm.lead.invalid_agent", "el asesor no existe o no está activo")
)

// Status es el estado del prospecto.
type Status string

// Estados.
const (
	StatusNew        Status = "new"
	StatusContacted  Status = "contacted"
	StatusInterested Status = "interested"
	StatusQuoting    Status = "quoting"
	StatusWon        Status = "won"
	StatusLost       Status = "lost"
	StatusFollowUp   Status = "follow_up"
)

// transitions sigue DECISIONS N31. Ganado y perdido son terminales.
var transitions = map[Status][]Status{ //nolint:gochecknoglobals // tabla inmutable de transiciones
	StatusNew:        {StatusContacted, StatusLost},
	StatusContacted:  {StatusInterested, StatusFollowUp, StatusLost},
	StatusInterested: {StatusQuoting, StatusFollowUp, StatusLost},
	StatusQuoting:    {StatusWon, StatusFollowUp, StatusLost},
	StatusFollowUp:   {StatusContacted, StatusInterested, StatusLost},
}

var sources = []string{"referral", "social", "website", "partner", "cross_sell", "call", "event", "other"} //nolint:gochecknoglobals // catálogo inmutable (flujos.md Flujo 1)

// Snapshot son los datos del prospecto.
type Snapshot struct {
	ID               uuid.UUID
	Name             string
	Email            string
	Phone            string
	Source           string
	LineOfBusinessID uuid.NullUUID
	AgentID          uuid.NullUUID
	PartyID          uuid.NullUUID
	Status           Status
	LostReason       string
	FollowUpOn       *kernel.Date
	ContactedAt      *time.Time
	AssignedAt       *time.Time
	Version          int32
}

// Lead es el agregado. Su estado solo cambia con sus métodos.
type Lead struct{ s Snapshot }

// New crea un prospecto con al menos un dato de contacto válido.
func New(s Snapshot, now time.Time) (*Lead, error) {
	s.Name, s.Email, s.Phone = strings.TrimSpace(s.Name), strings.TrimSpace(s.Email), strings.TrimSpace(s.Phone)
	if n := utf8.RuneCountInString(s.Name); n < 2 || n > 200 {
		return nil, ErrInvalidName
	}
	if s.Email == "" && s.Phone == "" {
		return nil, ErrContactRequired
	}
	if s.Email != "" {
		if a, err := mail.ParseAddress(s.Email); err != nil || a.Address != s.Email || len(s.Email) > 254 {
			return nil, ErrContactRequired
		}
	}
	if n := utf8.RuneCountInString(s.Phone); n > 30 || (s.Phone != "" && n < 7) {
		return nil, ErrContactRequired
	}
	if !slices.Contains(sources, s.Source) {
		return nil, ErrInvalidSource
	}
	s.Status, s.Version = StatusNew, 1
	if s.AgentID.Valid {
		at := now.UTC()
		s.AssignedAt = &at
	}
	return &Lead{s: s}, nil
}

// Rehydrate reconstruye el prospecto desde la persistencia.
func Rehydrate(s Snapshot) *Lead { return &Lead{s: s} }

// Snapshot devuelve una copia.
func (l *Lead) Snapshot() Snapshot { return l.s }

// LogValue evita que nombre y contacto lleguen a los logs.
func (l *Lead) LogValue() slog.Value { return slog.StringValue(l.s.ID.String()) }

func (l *Lead) transitionTo(to Status) error {
	if !slices.Contains(transitions[l.s.Status], to) {
		return ErrInvalidTransition
	}
	l.s.Status, l.s.FollowUpOn = to, nil
	return nil
}

// MarkContacted registra el contacto. La fecha del primero se conserva.
func (l *Lead) MarkContacted(now time.Time) error {
	if err := l.transitionTo(StatusContacted); err != nil {
		return err
	}
	if l.s.ContactedAt == nil {
		at := now.UTC()
		l.s.ContactedAt = &at
	}
	return nil
}

// MarkInterested registra que el prospecto quiere asesoría.
func (l *Lead) MarkInterested() error { return l.transitionTo(StatusInterested) }

// StartQuoting enlaza el cliente y pasa a cotización (DECISIONS N27).
func (l *Lead) StartQuoting(party uuid.UUID) error {
	if party == uuid.Nil {
		return ErrInvalidParty
	}
	if err := l.transitionTo(StatusQuoting); err != nil {
		return err
	}
	l.s.PartyID = uuid.NullUUID{UUID: party, Valid: true}
	return nil
}

// ScheduleFollowUp programa un seguimiento, hoy o después.
func (l *Lead) ScheduleFollowUp(on, today kernel.Date) error {
	if on.IsZero() || on.Before(today) {
		return ErrInvalidFollowUp
	}
	if err := l.transitionTo(StatusFollowUp); err != nil {
		return err
	}
	l.s.FollowUpOn = &on
	return nil
}

// MarkLost cierra el prospecto con su motivo.
func (l *Lead) MarkLost(reason string) error {
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 3 || n > 500 {
		return ErrInvalidReason
	}
	if err := l.transitionTo(StatusLost); err != nil {
		return err
	}
	l.s.LostReason = reason
	return nil
}

// MarkWon registra que el cliente aceptó una oferta (DECISIONS N28).
func (l *Lead) MarkWon() error { return l.transitionTo(StatusWon) }

// Assign cambia el asesor de un prospecto no terminado.
func (l *Lead) Assign(agent uuid.UUID, now time.Time) error {
	if l.s.Status == StatusWon || l.s.Status == StatusLost {
		return ErrInvalidTransition
	}
	if agent == uuid.Nil {
		return ErrInvalidAgent
	}
	at := now.UTC()
	l.s.AgentID, l.s.AssignedAt = uuid.NullUUID{UUID: agent, Valid: true}, &at
	return nil
}
