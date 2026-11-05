package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/marvinrepremarca/seguros/internal/platform/db"
)

// Health resume el estado de la mensajería entre tenants, sin contenido de mensajes.
type Health struct {
	// DeadLettersPending son mensajes fallidos sin reprocesar. Uno perdido puede dejar a un asesor con
	// acceso revocado o a una póliza sin cartera, así que cualquier valor mayor que cero amerita revisión.
	DeadLettersPending int64
	// Stuck son eventos que agotaron sus intentos de publicación y el relay ya no reclama.
	Stuck int64
	// OldestPending es el retraso del evento pendiente más antiguo.
	OldestPending time.Duration
}

// ReadHealth lee la salud de la mensajería (rol seguros_worker).
func ReadHealth(ctx context.Context, database *db.DB, at time.Time) (Health, error) {
	var h Health
	var seconds int64
	err := database.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT dead_letters_pending, outbox_stuck, outbox_oldest_pending_seconds FROM messaging.health($1)`, at.UTC()).
			Scan(&h.DeadLettersPending, &h.Stuck, &seconds)
	})
	if err != nil {
		return Health{}, fmt.Errorf("outbox: leyendo salud: %w", err)
	}
	h.OldestPending = time.Duration(seconds) * time.Second
	return h, nil
}
