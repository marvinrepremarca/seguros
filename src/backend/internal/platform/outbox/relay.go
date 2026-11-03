package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Relay publica los eventos pendientes del outbox. Varias instancias pueden correr a la vez: cada
// lote se reclama con FOR UPDATE SKIP LOCKED.
type Relay struct {
	db        *db.DB
	publisher message.Publisher
	clock     kernel.Clock
	logger    *slog.Logger
	batch     int
	interval  time.Duration
}

// RelayOptions configura el relay.
type RelayOptions struct {
	Batch    int           // máximo de mensajes por ciclo, default 100
	Interval time.Duration // espera entre lotes vacíos, default 500 ms
}

// NewRelay construye el relay.
func NewRelay(database *db.DB, publisher message.Publisher, clock kernel.Clock, logger *slog.Logger, opts RelayOptions) *Relay {
	if opts.Batch <= 0 {
		opts.Batch = 100
	}
	if opts.Interval <= 0 {
		opts.Interval = 500 * time.Millisecond
	}
	return &Relay{db: database, publisher: publisher, clock: clock, logger: logger, batch: opts.Batch, interval: opts.Interval}
}

// Run publica lotes hasta que ctx se cancele.
func (r *Relay) Run(ctx context.Context) error {
	for {
		n, err := r.RelayOnce(ctx)
		if err != nil && ctx.Err() == nil {
			r.logger.ErrorContext(ctx, "relay del outbox", "error", err)
		}
		if n > 0 && err == nil {
			continue // puede haber más pendientes
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(r.interval):
		}
	}
}

type pending struct {
	id         uuid.UUID
	tenant     uuid.UUID
	topic      string
	payload    []byte
	occurredAt time.Time
}

// RelayOnce publica hasta Batch mensajes, cada uno en su propia transacción (reclamar →
// publicar → marcar): el bloqueo de la fila dura solo lo que tardan sus consumidores y nunca se
// acerca al idle_in_transaction_session_timeout. Devuelve cuántos publicó. Un mensaje cuya
// publicación falla se marca con el error y queda para el siguiente ciclo.
func (r *Relay) RelayOnce(ctx context.Context) (int, error) {
	published := 0
	for range r.batch {
		found, ok, err := r.relayOne(ctx)
		if err != nil {
			return published, err
		}
		if !found || !ok {
			break // vacío, o un fallo: se reintenta en el siguiente ciclo tras la espera
		}
		published++
	}
	return published, nil
}

func (r *Relay) relayOne(ctx context.Context) (found, ok bool, err error) {
	err = r.db.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		var p pending
		err := tx.QueryRow(ctx, `SELECT id, tenant_id, topic, payload, occurred_at FROM messaging.claim_outbox(1)`).
			Scan(&p.id, &p.tenant, &p.topic, &p.payload, &p.occurredAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("outbox: reclamando evento: %w", err)
		}
		found = true
		msg := message.NewMessage(p.id.String(), p.payload)
		msg.Metadata.Set(MetaTenantID, p.tenant.String())
		msg.Metadata.Set(MetaOccurredAt, p.occurredAt.UTC().Format(time.RFC3339Nano))
		msg.SetContext(ctx)
		// Bloquea hasta que todos los consumidores del tópico confirmen (GoChannel con
		// BlockPublishUntilSubscriberAck); con RabbitMQ (fase 2) confirma el broker.
		if perr := r.publisher.Publish(p.topic, msg); perr != nil {
			r.logger.WarnContext(ctx, "publicando evento", "topic", p.topic, "event_id", p.id.String(), "error", perr)
			if _, ferr := tx.Exec(ctx, `SELECT messaging.mark_failed($1, $2)`, p.id, perr.Error()); ferr != nil {
				return fmt.Errorf("outbox: marcando fallo: %w", ferr)
			}
			return nil
		}
		if _, err := tx.Exec(ctx, `SELECT messaging.mark_published($1, $2)`, []uuid.UUID{p.id}, r.clock.Now().UTC()); err != nil {
			return fmt.Errorf("outbox: marcando publicado: %w", err)
		}
		ok = true
		return nil
	})
	return found, ok, err
}
