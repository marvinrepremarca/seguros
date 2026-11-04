// Package policiesevents contiene los consumidores de eventos de policies.
package policiesevents

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/app"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Binder da la unidad de trabajo sobre la transacción del consumidor, que incluye el inbox.
type Binder interface {
	Bind(ctx context.Context, tx pgx.Tx) (app.UnitOfWork, error)
}

// PortfolioTransfer procesa por lotes el traspaso de pólizas (DECISIONS N11). Módulo núcleo: sin flag.
func PortfolioTransfer(store Binder, clock kernel.Clock) outbox.Consumer {
	return outbox.Consumer{
		Name:  "policies.portfolio-transfer",
		Topic: contracts.TopicPortfolioTransferRequested,
		Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
			var ev contracts.PortfolioTransferRequested
			if err := e.Decode(&ev); err != nil {
				return err
			}
			// Quien pidió el traspaso es el autor en el historial y en la bitácora.
			ctx = reqctx.WithUserID(ctx, kernel.UserID(ev.RequestedBy))
			uow, err := store.Bind(ctx, tx)
			if err != nil {
				return err
			}
			return app.TransferPoliciesBatch(ctx, uow, clock, ev)
		},
	}
}
