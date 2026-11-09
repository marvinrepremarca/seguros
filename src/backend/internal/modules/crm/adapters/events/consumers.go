// Package crmevents contiene los consumidores de crm.
package crmevents

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	crmsqlc "github.com/marvinrepremarca/seguros/internal/modules/crm/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/crm/app"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	quoting "github.com/marvinrepremarca/seguros/internal/modules/quoting/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Binder da la unidad de trabajo sobre la transacción del consumidor, que incluye el inbox.
type Binder interface {
	Bind(ctx context.Context, tx pgx.Tx) (app.UnitOfWork, error)
}

// Consumers devuelve los consumidores de crm.
func Consumers(store Binder, recorder *audit.Recorder, clock kernel.Clock) []outbox.Consumer {
	return []outbox.Consumer{
		{
			Name: "crm.quote-accepted", Topic: quoting.TopicQuoteAccepted, Flag: flags.CrmModule,
			Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
				var ev quoting.QuoteAccepted
				if err := e.Decode(&ev); err != nil {
					return err
				}
				if ev.LeadID == nil {
					return nil
				}
				uow, err := store.Bind(ctx, tx)
				if err != nil {
					return err
				}
				return app.MarkWon(ctx, uow, clock, *ev.LeadID, ev.AgentID, ev.HolderPartyID)
			},
		},
		{
			// Una sola sentencia: no depende del tamaño de la cartera (DECISIONS N11).
			Name: "crm.portfolio-transfer", Topic: policies.TopicPortfolioTransferRequested, Flag: flags.CrmModule,
			Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
				var ev policies.PortfolioTransferRequested
				if err := e.Decode(&ev); err != nil {
					return err
				}
				if ev.RequestedAt.IsZero() {
					return fmt.Errorf("crm: traspaso sin corte en el evento %s", e.ID)
				}
				at := e.OccurredAt
				n, err := crmsqlc.New(tx).TransferLeads(ctx, crmsqlc.TransferLeadsParams{
					ToAgent: uuid.NullUUID{UUID: ev.ToAgentID, Valid: true}, At: &at, Actor: uuid.NullUUID{UUID: ev.RequestedBy, Valid: true},
					TenantID: uuid.UUID(e.TenantID), FromAgent: uuid.NullUUID{UUID: ev.FromAgentID, Valid: true}, RequestedAt: &ev.RequestedAt,
				})
				if err != nil {
					return fmt.Errorf("crm: traspasando prospectos: %w", err)
				}
				if n == 0 {
					return nil
				}
				ctx = reqctx.WithUserID(ctx, kernel.UserID(ev.RequestedBy))
				return recorder.Record(ctx, tx, audit.Entry{
					Action: "crm.portfolio.transferred", ResourceType: "crm.lead", ResourceID: ev.FromAgentID.String(), Outcome: audit.OutcomeSuccess,
					Details: map[string]string{"new": ev.ToAgentID.String(), "requestId": ev.RequestID.String(), "count": strconv.FormatInt(n, 10)},
				})
			},
		},
	}
}
