// Package policiesevents contiene los consumidores de eventos de policies.
package policiesevents

import (
	"context"

	"github.com/jackc/pgx/v5"

	billing "github.com/marvinrepremarca/seguros/internal/modules/billing/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/app"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	renewals "github.com/marvinrepremarca/seguros/internal/modules/renewals/contracts"
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

// Arrears aplica la mora y su salida. Módulo núcleo: sin flag; los eventos solo existen con billing
// encendido, y policies no depende de que lo esté.
func Arrears(store Binder, clock kernel.Clock) []outbox.Consumer {
	return []outbox.Consumer{
		{
			Name: "policies.mark-in-arrears", Topic: billing.TopicInstallmentOverdue,
			Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
				var ev billing.InstallmentOverdue
				if err := e.Decode(&ev); err != nil {
					return err
				}
				uow, err := store.Bind(ctx, tx)
				if err != nil {
					return err
				}
				return app.MarkInArrears(ctx, uow, clock, ev.PolicyID)
			},
		},
		{
			Name: "policies.reinstate-cleared", Topic: billing.TopicArrearsCleared,
			Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
				var ev billing.ArrearsCleared
				if err := e.Decode(&ev); err != nil {
					return err
				}
				uow, err := store.Bind(ctx, tx)
				if err != nil {
					return err
				}
				return app.ReinstateAfterPayment(ctx, uow, clock, ev.PolicyID)
			},
		},
		{
			Name: "policies.reinstate", Topic: billing.TopicPaymentApplied,
			Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
				var ev billing.PaymentApplied
				if err := e.Decode(&ev); err != nil {
					return err
				}
				if ev.InArrears {
					return nil
				}
				uow, err := store.Bind(ctx, tx)
				if err != nil {
					return err
				}
				return app.ReinstateAfterPayment(ctx, uow, clock, ev.PolicyID)
			},
		},
	}
}

// Renewal cierra la póliza anterior cuando se expide su nueva vigencia o el cliente no renueva.
func Renewal(store Binder, clock kernel.Clock) []outbox.Consumer {
	return []outbox.Consumer{
		{
			Name: "policies.renew-predecessor", Topic: contracts.TopicPolicyIssued,
			Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
				var ev contracts.PolicyIssued
				if err := e.Decode(&ev); err != nil {
					return err
				}
				if ev.RenewedFromPolicyID == nil {
					return nil
				}
				uow, err := store.Bind(ctx, tx)
				if err != nil {
					return err
				}
				return app.RenewPredecessor(ctx, uow, clock, *ev.RenewedFromPolicyID, ev.PolicyID)
			},
		},
		{
			Name: "policies.mark-not-renewed", Topic: renewals.TopicRenewalLost,
			Handle: func(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
				var ev renewals.RenewalLost
				if err := e.Decode(&ev); err != nil {
					return err
				}
				uow, err := store.Bind(ctx, tx)
				if err != nil {
					return err
				}
				return app.MarkNotRenewed(ctx, uow, clock, ev.PolicyID)
			},
		},
	}
}
