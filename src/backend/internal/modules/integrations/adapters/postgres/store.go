// Package postgres guarda la bitácora de llamadas a los conectores.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	integrationssqlc "github.com/marvinrepremarca/seguros/internal/modules/integrations/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Store implementa app.CallLog.
type Store struct{ db *db.DB }

// NewStore construye el Store.
func NewStore(database *db.DB) *Store { return &Store{db: database} }

// Record implementa app.CallLog.
func (s *Store) Record(ctx context.Context, connector, operation, outcome string, at time.Time) error {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return db.ErrNoTenant
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("integrations: id: %w", err)
	}
	actor := uuid.NullUUID{}
	if u, ok := reqctx.UserID(ctx); ok {
		actor = uuid.NullUUID{UUID: uuid.UUID(u), Valid: true}
	}
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		err := integrationssqlc.New(tx).InsertCall(ctx, integrationssqlc.InsertCallParams{ID: id, TenantID: uuid.UUID(tenant), Connector: connector, Operation: operation, Outcome: outcome, At: at, Actor: actor})
		if err != nil {
			return fmt.Errorf("integrations: registrando llamada: %w", err)
		}
		return nil
	})
}
