// Package app contiene los casos de uso de notificaciones.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"time"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/notifications/domain"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

const (
	sendBatch = 50
	// sendLease cubre un lote completo con los plazos del SMTP.
	sendLease = 30 * time.Minute
)

// Message es un aviso en la bandeja de salida.
type Message struct {
	ID, PolicyID uuid.UUID
	Template     string
	Channel      string
	Params       json.RawMessage
	Status       string
	SkipReason   string
	Attempts     int32
	CreatedAt    time.Time
	SentAt       *time.Time
}

// Repository persiste avisos en el tenant del contexto.
type Repository interface {
	Insert(ctx context.Context, id uuid.UUID, template string, policy uuid.UUID, params []byte, dedupe string, at time.Time) error
	Claim(ctx context.Context, now, leaseUntil time.Time, limit int32) ([]Message, error)
	MarkSent(ctx context.Context, id, party uuid.UUID, at time.Time) error
	MarkSkipped(ctx context.Context, id uuid.UUID, party uuid.NullUUID, reason domain.Skip, at time.Time) error
	MarkAttempt(ctx context.Context, id uuid.UUID, status string, attempts int32, next *time.Time, at time.Time) error
	Retry(ctx context.Context, id uuid.UUID, at time.Time) error
	List(ctx context.Context, before uuid.NullUUID, limit int32) ([]Message, error)
}

// UnitOfWork es una transacción del tenant.
type UnitOfWork interface {
	Records() Repository
	Audit(ctx context.Context, e audit.Entry) error
}

// Store abre unidades de trabajo.
type Store interface {
	WithinTenant(ctx context.Context, fn func(ctx context.Context, uow UnitOfWork) error) error
}

// Sender entrega un correo. Otros canales (SMS, WhatsApp) irán detrás de este mismo puerto.
type Sender interface {
	Send(ctx context.Context, to string, e domain.Email) error
}

// Service agrupa los casos de uso.
type Service struct {
	Store    Store
	Sender   Sender
	Policies policies.Directory
	Parties  parties.ContactView
	Clock    kernel.Clock
	Logger   *slog.Logger
}

// Enqueue guarda un aviso; una clave repetida no tiene efecto.
func Enqueue(ctx context.Context, uow UnitOfWork, clock kernel.Clock, template string, policy uuid.UUID, p domain.Params, dedupe string) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("notifications: parámetros: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("notifications: id: %w", err)
	}
	return uow.Records().Insert(ctx, id, template, policy, raw, dedupe, clock.Now())
}

// consentRevoked es verdadero si la última decisión del titular para alguna finalidad fue revocar.
// Los registros llegan del más reciente al más antiguo.
func consentRevoked(consents []parties.HabeasConsent) bool {
	seen := map[string]bool{}
	for _, c := range consents {
		if seen[c.Purpose] {
			continue
		}
		seen[c.Purpose] = true
		if c.Status == "revoked" {
			return true
		}
	}
	return false
}

// SendPending entrega los avisos pendientes del tenant. Un fallo de un aviso no detiene los demás.
func (s *Service) SendPending(ctx context.Context) error {
	var pending []Message
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) (err error) {
		now := s.Clock.Now()
		pending, err = uow.Records().Claim(ctx, now, now.Add(sendLease), sendBatch)
		return err
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, m := range pending {
		errs = append(errs, s.deliver(ctx, m))
	}
	return errors.Join(errs...)
}

func (s *Service) skip(ctx context.Context, m Message, party uuid.NullUUID, why domain.Skip) error {
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		return uow.Records().MarkSkipped(ctx, m.ID, party, why, s.Clock.Now())
	})
}

func (s *Service) deliver(ctx context.Context, m Message) error {
	facts, ok, err := s.Policies.Facts(ctx, m.PolicyID)
	if err != nil {
		return fmt.Errorf("notifications: póliza: %w", err)
	}
	if !ok {
		return s.skip(ctx, m, uuid.NullUUID{}, domain.SkipNoRecipient)
	}
	party := uuid.NullUUID{UUID: facts.HolderPartyID, Valid: true}
	holder, ok, err := s.Parties.Contact(ctx, facts.HolderPartyID)
	if err != nil {
		return fmt.Errorf("notifications: titular: %w", err)
	}
	if !ok {
		return s.skip(ctx, m, party, domain.SkipNoRecipient)
	}
	if consentRevoked(holder.Consents) {
		return s.skip(ctx, m, party, domain.SkipConsentRevoked)
	}
	if _, err := mail.ParseAddress(holder.Email); err != nil {
		return s.skip(ctx, m, party, domain.SkipNoEmail)
	}
	var params domain.Params
	if err := json.Unmarshal(m.Params, &params); err != nil {
		return fmt.Errorf("notifications: parámetros: %w", err)
	}
	email, ok := domain.Render(m.Template, holder.Name, facts.Number, params)
	if !ok {
		return s.attempt(ctx, m, domain.MaxAttempts)
	}
	if err := s.Sender.Send(ctx, holder.Email, email); err != nil {
		return s.attempt(ctx, m, m.Attempts+1)
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		return uow.Records().MarkSent(ctx, m.ID, facts.HolderPartyID, s.Clock.Now())
	})
}

// attempt cuenta un intento fallido; al agotarlos el aviso queda fallido y visible en la bandeja.
// El error de envío no se registra: puede incluir la dirección del cliente.
func (s *Service) attempt(ctx context.Context, m Message, attempts int32) error {
	status := "pending"
	if attempts >= domain.MaxAttempts {
		status = "failed"
	}
	var next *time.Time
	if status == "pending" {
		at := s.Clock.Now().Add(domain.Backoff(attempts))
		next = &at
	}
	s.Logger.WarnContext(ctx, "aviso sin entregar", "message_id", m.ID, "attempts", attempts)
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		return uow.Records().MarkAttempt(ctx, m.ID, status, attempts, next, s.Clock.Now())
	})
}

// Page es una página de avisos.
type Page struct {
	Items []Message
	Next  uuid.NullUUID
}

// List devuelve la bandeja de salida.
func (s *Service) List(ctx context.Context, before uuid.NullUUID, limit int32) (Page, error) {
	if err := authz.Require(ctx, authz.NotificationRead); err != nil {
		return Page{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []Message
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) (err error) {
		rows, err = uow.Records().List(ctx, before, limit+1)
		return err
	})
	if err != nil {
		return Page{}, err
	}
	if int32(len(rows)) <= limit { //nolint:gosec // acotado por limit
		return Page{Items: rows}, nil
	}
	rows = rows[:limit]
	return Page{Items: rows, Next: uuid.NullUUID{UUID: rows[limit-1].ID, Valid: true}}, nil
}

// Retry devuelve a la cola un aviso fallido.
func (s *Service) Retry(ctx context.Context, id uuid.UUID) error {
	if err := authz.Require(ctx, authz.NotificationManage); err != nil {
		return err
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Records().Retry(ctx, id, s.Clock.Now()); err != nil {
			return err
		}
		return uow.Audit(ctx, audit.Entry{Action: "notifications.message.retried", ResourceType: "notification", ResourceID: id.String(), Outcome: audit.OutcomeSuccess})
	})
}
