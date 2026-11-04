// Package partiesevents contiene los consumidores de eventos de parties.
package partiesevents

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	partiessqlc "github.com/marvinrepremarca/seguros/internal/modules/parties/adapters/postgres/sqlc"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

// PolicyScope mantiene la proyección que define los clientes de cada asesor (DECISIONS N10). Módulo
// núcleo: sin flag. La versión de la póliza descarta entregas viejas o fuera de orden.
func PolicyScope() outbox.Consumer {
	return outbox.Consumer{
		Name:  "parties.policy-scope",
		Topic: policies.TopicPolicyPartiesChanged,
		Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
			var ev policies.PolicyPartiesChanged
			if err := e.Decode(&ev); err != nil {
				return err
			}
			agent := uuid.NullUUID{}
			if ev.AgentID != nil {
				agent = uuid.NullUUID{UUID: *ev.AgentID, Valid: true}
			}
			if err := partiessqlc.New(tx).UpsertPolicyScope(ctx, partiessqlc.UpsertPolicyScopeParams{
				TenantID: uuid.UUID(e.TenantID), PolicyID: ev.PolicyID, AgentID: agent, PartyIds: ev.PartyIDs,
				PolicyVersion: ev.PolicyVersion, UpdatedAt: e.OccurredAt,
			}); err != nil {
				return fmt.Errorf("parties: actualizando alcance de la póliza: %w", err)
			}
			return nil
		},
	}
}
