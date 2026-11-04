package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// ErrDeadLetterNotPending indica que el mensaje no existe en el tenant o ya se reprocesó.
var ErrDeadLetterNotPending = errors.New("outbox: mensaje inexistente o ya reprocesado")

// ErrDeadLetterTooOld indica que el mensaje supera la antigüedad de reproceso sin force.
var ErrDeadLetterTooOld = errors.New("outbox: mensaje demasiado antiguo para reprocesarlo sin force")

// DeadLetter resume un mensaje fallido, sin su contenido ni su error.
type DeadLetter struct {
	ID       uuid.UUID
	Consumer string
	Topic    string
	EventID  uuid.UUID
	FailedAt time.Time
}

const maxDeadLetters = 200

// ListDeadLetters lista los mensajes pendientes de reproceso del tenant del contexto.
func ListDeadLetters(ctx context.Context, database *db.DB) ([]DeadLetter, error) {
	var out []DeadLetter
	err := database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, consumer, topic, event_id, failed_at FROM messaging.dead_letters
			WHERE retried_at IS NULL ORDER BY failed_at LIMIT $1`, maxDeadLetters)
		if err != nil {
			return fmt.Errorf("outbox: listando mensajes fallidos: %w", err)
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[DeadLetter])
		if err != nil {
			return fmt.Errorf("outbox: leyendo mensajes fallidos: %w", err)
		}
		return nil
	})
	return out, err
}

// maxRetryAge limita el reproceso sin force: un evento viejo pudo perder vigencia, por ejemplo el
// destino de un traspaso que ya no es asesor activo.
const maxRetryAge = 7 * 24 * time.Hour

// RetryDeadLetter vuelve a publicar el evento del mensaje. Cada mensaje se reprocesa una vez: si vuelve
// a fallar, queda como un mensaje nuevo. record deja el rastro en la misma transacción.
func RetryDeadLetter(ctx context.Context, database *db.DB, id uuid.UUID, at time.Time, force bool, record func(context.Context, pgx.Tx) error) error {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return db.ErrNoTenant
	}
	return database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		var failedAt time.Time
		err := tx.QueryRow(ctx, `SELECT failed_at FROM messaging.dead_letters WHERE id = $1 AND retried_at IS NULL`, id).Scan(&failedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDeadLetterNotPending
		}
		if err != nil {
			return fmt.Errorf("outbox: leyendo mensaje: %w", err)
		}
		if !force && at.Sub(failedAt) > maxRetryAge {
			return ErrDeadLetterTooOld
		}
		var retried bool
		if err := tx.QueryRow(ctx, `SELECT messaging.retry_dead_letter($1, $2, $3)`, uuid.UUID(tenant), id, at.UTC()).Scan(&retried); err != nil {
			return fmt.Errorf("outbox: reprocesando mensaje: %w", err)
		}
		if !retried {
			return ErrDeadLetterNotPending
		}
		return record(ctx, tx)
	})
}
