// Package quotingevents contiene los consumidores de quoting.
package quotingevents

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	quotingsqlc "github.com/marvinrepremarca/seguros/internal/modules/quoting/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Consumers devuelve los consumidores de quoting.
func Consumers(recorder *audit.Recorder) []outbox.Consumer {
	return []outbox.Consumer{{
		Name: "quoting.portfolio-transfer", Topic: policies.TopicPortfolioTransferRequested, Flag: flags.QuotingModule,
		Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
			var ev policies.PortfolioTransferRequested
			if err := e.Decode(&ev); err != nil {
				return err
			}
			if ev.RequestedAt.IsZero() {
				return fmt.Errorf("quoting: traspaso sin corte en el evento %s", e.ID)
			}
			n, err := quotingsqlc.New(tx).TransferRequests(ctx, quotingsqlc.TransferRequestsParams{
				ToAgent: ev.ToAgentID, At: e.OccurredAt, Actor: uuid.NullUUID{UUID: ev.RequestedBy, Valid: true},
				TenantID: uuid.UUID(e.TenantID), FromAgent: ev.FromAgentID, RequestedAt: ev.RequestedAt,
			})
			if err != nil {
				return fmt.Errorf("quoting: traspasando solicitudes: %w", err)
			}
			if n == 0 {
				return nil
			}
			ctx = reqctx.WithUserID(ctx, kernel.UserID(ev.RequestedBy))
			return recorder.Record(ctx, tx, audit.Entry{
				Action: "quoting.portfolio.transferred", ResourceType: "quoting.request", ResourceID: ev.FromAgentID.String(), Outcome: audit.OutcomeSuccess,
				Details: map[string]string{"new": ev.ToAgentID.String(), "requestId": ev.RequestID.String(), "count": strconv.FormatInt(n, 10)},
			})
		},
	}}
}
