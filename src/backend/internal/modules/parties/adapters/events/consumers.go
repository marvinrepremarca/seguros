// Package partiesevents contiene los consumidores de eventos de parties.
package partiesevents

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	partiessqlc "github.com/marvinrepremarca/seguros/internal/modules/parties/adapters/postgres/sqlc"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// PortfolioTransfer pasa al asesor nuevo los clientes de los que era responsable el anterior
// (DECISIONS N11). Una sola sentencia: no depende del tamaño de la cartera.
func PortfolioTransfer(recorder *audit.Recorder) outbox.Consumer {
	return outbox.Consumer{
		Name:  "parties.portfolio-transfer",
		Topic: policies.TopicPortfolioTransferRequested,
		Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
			var ev policies.PortfolioTransferRequested
			if err := e.Decode(&ev); err != nil {
				return err
			}
			// Un motivo fuera del catálogo o sin corte no se aplica: iría al historial o movería todo.
			if _, ok := kernel.NewTransferReason(ev.Reason, otherNote(ev.Reason)); !ok || ev.RequestedAt.IsZero() {
				return fmt.Errorf("parties: traspaso inválido en el evento %s", e.ID)
			}
			// Quien pidió el traspaso es el autor en el historial y en la bitácora.
			ctx = reqctx.WithUserID(ctx, kernel.UserID(ev.RequestedBy))
			n, err := partiessqlc.New(tx).TransferResponsibleAgent(ctx, partiessqlc.TransferResponsibleAgentParams{
				ToAgent: ev.ToAgentID, UpdatedAt: e.OccurredAt, TenantID: uuid.UUID(e.TenantID),
				FromAgent: uuid.NullUUID{UUID: ev.FromAgentID, Valid: true}, Reason: ev.Reason, RequestedAt: ev.RequestedAt,
				Actor: uuid.NullUUID{UUID: ev.RequestedBy, Valid: true},
			})
			if err != nil {
				return fmt.Errorf("parties: traspasando cartera: %w", err)
			}
			if n == 0 {
				return nil // lote de pólizas repetido: el evento se republica por lotes
			}
			return recorder.Record(ctx, tx, audit.Entry{
				Action: "parties.portfolio.transferred", ResourceType: "parties.party", ResourceID: ev.FromAgentID.String(), Outcome: audit.OutcomeSuccess,
				Details: map[string]string{
					"new": ev.ToAgentID.String(), "reason": ev.Reason, "requestedBy": ev.RequestedBy.String(), "requestId": ev.RequestID.String(), "clients": strconv.FormatInt(n, 10),
				},
			})
		},
	}
}

// PolicyScope mantiene la proyección que define los clientes de cada asesor (DECISIONS N10, N11): una
// póliza terminada deja de dar acceso. Módulo
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
			if ev.AgentID != nil && ev.GrantsAccess {
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

// otherNote satisface la nota obligatoria de "other": la nota real no viaja en el evento.
func otherNote(code string) string {
	if code == kernel.TransferOther {
		return "Ver solicitud de traspaso"
	}
	return ""
}
