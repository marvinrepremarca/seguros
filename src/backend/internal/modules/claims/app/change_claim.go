package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/claims/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/claims/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// change carga, aplica, guarda, publica el cambio de estado y audita, en una transacción.
func (s *Service) change(ctx context.Context, id uuid.UUID, action string, details map[string]string, fn func(context.Context, UnitOfWork, *domain.Claim) error) (View, error) {
	var snap domain.Snapshot
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		c, err := uow.Claims().Get(ctx, id)
		if err != nil {
			return err
		}
		from := c.Snapshot().Status
		if err := fn(ctx, uow, c); err != nil {
			return err
		}
		if err := uow.Claims().Save(ctx, c, s.Clock.Now().UTC()); err != nil {
			return err
		}
		snap = c.Snapshot()
		e := entry(action, id)
		e.Details = map[string]string{"from": string(from), "to": string(snap.Status)}
		for k, v := range details {
			e.Details[k] = v
		}
		if from != snap.Status {
			if err := uow.Publish(ctx, contracts.TopicClaimStatusChanged, contracts.ClaimStatusChanged{ClaimID: id, PolicyID: snap.PolicyID, From: string(from), To: string(snap.Status)}); err != nil {
				return err
			}
		}
		return uow.Audit(ctx, e)
	})
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, snap)
}

// AdvanceInput pide un cambio de estado.
type AdvanceInput struct {
	To, InsurerClaimNumber, Support string
}

// Advance cambia el estado, excepto el pago.
func (s *Service) Advance(ctx context.Context, id uuid.UUID, in AdvanceInput) (View, error) {
	if err := authz.Require(ctx, authz.ClaimManage); err != nil {
		return View{}, err
	}
	return s.change(ctx, id, "claims.claim.status_changed", nil, func(_ context.Context, _ UnitOfWork, c *domain.Claim) error {
		switch domain.Status(in.To) {
		case domain.StatusNotified:
			return c.Notify(in.InsurerClaimNumber, s.Clock.Now())
		case domain.StatusDocumenting:
			return c.StartDocumentation()
		case domain.StatusEvaluating:
			return c.StartEvaluation()
		case domain.StatusApproved:
			return c.Approve(in.Support)
		case domain.StatusObjected:
			return c.Object(in.Support)
		case domain.StatusClosed:
			return c.Close()
		case domain.StatusReported, domain.StatusPaid:
			return domain.ErrInvalidTransition
		}
		return domain.ErrInvalidTransition
	})
}

func money(amount decimal.Decimal, c *domain.Claim) (kernel.Money, error) {
	m, err := kernel.NewMoney(amount, c.Snapshot().Currency)
	if err != nil {
		return kernel.Money{}, domain.ErrInvalidAmount
	}
	return m, nil
}

// SetClaimed fija el valor reclamado con su motivo (DECISIONS N32): el tope del pago depende de él. El
// motivo es texto libre: queda en las gestiones; la bitácora guarda valores y autor.
func (s *Service) SetClaimed(ctx context.Context, id uuid.UUID, amount decimal.Decimal, reason string) (View, error) {
	if err := authz.Require(ctx, authz.ClaimManage); err != nil {
		return View{}, err
	}
	if err := domain.ValidateNote(reason); err != nil {
		return View{}, err
	}
	details := map[string]string{"amount": amount.String()}
	return s.change(ctx, id, "claims.claim.claimed_set", details, func(ctx context.Context, uow UnitOfWork, c *domain.Claim) error {
		m, err := money(amount, c)
		if err != nil {
			return err
		}
		if prev := c.Snapshot().ClaimedAmount; prev != nil {
			details["previous"] = prev.Amount().String()
		}
		if err := c.SetClaimed(m); err != nil {
			return err
		}
		return uow.Claims().AddNote(ctx, id, "Valor reclamado "+amount.String()+". Motivo: "+reason, s.Clock.Now().UTC())
	})
}

// RecordPayment registra el pago informado por la aseguradora, hasta lo reclamado.
func (s *Service) RecordPayment(ctx context.Context, id uuid.UUID, amount decimal.Decimal) (View, error) {
	if err := authz.Require(ctx, authz.ClaimManage); err != nil {
		return View{}, err
	}
	return s.change(ctx, id, "claims.claim.paid", map[string]string{"amount": amount.String()}, func(_ context.Context, _ UnitOfWork, c *domain.Claim) error {
		m, err := money(amount, c)
		if err != nil {
			return err
		}
		return c.RecordPayment(m, false)
	})
}

// OverridePayment registra un pago mayor a lo reclamado (DECISIONS N30). Motivo y soporte son texto
// libre: quedan en las gestiones del siniestro; la bitácora guarda valores y autor.
func (s *Service) OverridePayment(ctx context.Context, id uuid.UUID, amount decimal.Decimal, reason, support string) (View, error) {
	if err := authz.Require(ctx, authz.ClaimOverridePayment); err != nil {
		return View{}, err
	}
	note := "Pago aprobado por encima de lo reclamado. Motivo: " + reason + ". Soporte: " + support
	if err := domain.ValidateNote(reason); err != nil {
		return View{}, err
	}
	if err := domain.ValidateNote(support); err != nil {
		return View{}, err
	}
	return s.change(ctx, id, "claims.claim.paid_override", map[string]string{"amount": amount.String(), "override": "true"}, func(ctx context.Context, uow UnitOfWork, c *domain.Claim) error {
		m, err := money(amount, c)
		if err != nil {
			return err
		}
		if err := c.RecordPayment(m, true); err != nil {
			return err
		}
		return uow.Claims().AddNote(ctx, id, note, s.Clock.Now().UTC())
	})
}
