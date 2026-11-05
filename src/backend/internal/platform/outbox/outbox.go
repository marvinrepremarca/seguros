// Package outbox implementa los eventos de integración entre módulos (ARCHITECTURE.md §5):
//
//   - Writer.Add inserta el evento en messaging.outbox dentro de la MISMA transacción del cambio
//     (db.WithTenantTx), de modo que no hay cambio sin evento ni evento sin cambio.
//   - Relay (cmd/worker) reclama los pendientes y los publica en Watermill; los marca publicados
//     solo cuando todos los consumidores los confirmaron (at-least-once).
//   - Dispatcher registra consumidores idempotentes: cada evento se procesa una vez por
//     consumidor (messaging.inbox en la misma transacción de sus efectos), verificando antes el
//     flag del módulo para el tenant del evento.
//
// Los eventos transportan IDs, nunca datos Restringidos (rules/security.md).
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Metadatos Watermill de cada mensaje.
const (
	MetaTenantID   = "tenant_id"
	MetaOccurredAt = "occurred_at"
	// MetaTargetConsumers limita la entrega a estos consumidores, separados por coma (reproceso).
	// Vacío: todos los del tópico.
	MetaTargetConsumers = "target_consumers"
)

// maxPayload coincide con el CHECK de messaging.outbox.
const maxPayload = 16 << 10

var topicPattern = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*$`) //nolint:gochecknoglobals // regexp inmutable

// Errores del outbox.
var (
	ErrInvalidTopic   = errors.New("outbox: tópico inválido (<modulo>.<evento_snake>)")
	ErrPayloadTooLong = errors.New("outbox: payload demasiado grande (los eventos llevan IDs)")
	ErrNoTenant       = errors.New("outbox: evento sin tenant en el contexto")
)

// Event es un evento de integración ya persistido.
type Event struct {
	ID         uuid.UUID
	TenantID   kernel.TenantID
	Topic      string
	OccurredAt time.Time
	Payload    json.RawMessage
}

// Decode deserializa el payload en v (el tipo publicado en el contracts del módulo emisor).
func (e Event) Decode(v any) error {
	if err := json.Unmarshal(e.Payload, v); err != nil {
		return fmt.Errorf("outbox: payload de %s: %w", e.Topic, err)
	}
	return nil
}

// Writer escribe eventos en el outbox.
type Writer struct {
	clock kernel.Clock
}

// NewWriter construye el Writer.
func NewWriter(clock kernel.Clock) *Writer { return &Writer{clock: clock} }

// Add inserta un evento en tx (abierta con db.WithTenantTx para el tenant del contexto) y devuelve
// su id (UUIDv7, que los consumidores usan como clave de idempotencia).
func (w *Writer) Add(ctx context.Context, tx pgx.Tx, topic string, payload any) (uuid.UUID, error) {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return uuid.Nil, ErrNoTenant
	}
	if !topicPattern.MatchString(topic) || len(topic) > 120 {
		return uuid.Nil, fmt.Errorf("%w: %q", ErrInvalidTopic, topic)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: serializando %s: %w", topic, err)
	}
	if len(b) > maxPayload || len(b) == 0 || b[0] != '{' {
		return uuid.Nil, fmt.Errorf("%w (o no es un objeto JSON): %s", ErrPayloadTooLong, topic)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: generando id: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO messaging.outbox (id, tenant_id, topic, payload, occurred_at) VALUES ($1, $2, $3, $4, $5)`,
		id, uuid.UUID(tenant), topic, b, w.clock.Now().UTC(),
	); err != nil {
		return uuid.Nil, fmt.Errorf("outbox: insertando %s: %w", topic, err)
	}
	return id, nil
}
