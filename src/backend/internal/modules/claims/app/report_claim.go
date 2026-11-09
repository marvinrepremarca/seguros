package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/claims/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/claims/domain"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ReportInput son los datos del aviso del cliente.
type ReportInput struct {
	PolicyID                         uuid.UUID
	OccurredOn                       kernel.Date
	Location, ClaimType, Description string
	ClaimedAmount                    *decimal.Decimal
}

func policyView(f policies.PolicyFacts) domain.PolicyView {
	// Lista positiva: un estado desconocido genera alerta. Vencida o renovada sí estuvo en vigor; la
	// fecha de ocurrencia decide con la alerta de vigencia.
	inForce := false
	switch f.Status {
	case policies.StatusActive, policies.StatusInArrears, policies.StatusExpired, policies.StatusRenewed, policies.StatusNotRenewed:
		inForce = true
	}
	return domain.PolicyView{InForce: inForce, InArrears: f.Status == policies.StatusInArrears, From: f.EffectiveFrom, To: f.EffectiveTo}
}

// Report registra el siniestro. Un asesor solo reporta sobre sus pólizas (DECISIONS N29).
func (s *Service) Report(ctx context.Context, in ReportInput) (View, error) {
	if err := authz.Require(ctx, authz.ClaimReport); err != nil {
		return View{}, err
	}
	facts, found, err := s.Policies.Facts(ctx, in.PolicyID)
	if err != nil {
		return View{}, fmt.Errorf("claims: consultando póliza: %w", err)
	}
	if !found {
		return View{}, domain.ErrPolicyNotFound
	}
	if authz.Require(ctx, authz.ClaimRead) != nil {
		user, err := currentUser(ctx)
		if err != nil {
			return View{}, err
		}
		if !facts.AgentID.Valid || facts.AgentID.UUID != user {
			return View{}, domain.ErrPolicyNotFound
		}
	}
	id, err := uuid.NewV7()
	if err != nil {
		return View{}, fmt.Errorf("claims: %w", err)
	}
	draft := domain.Snapshot{
		ID: id, PolicyID: in.PolicyID, PolicyNumber: facts.Number, InsurerID: facts.InsurerID, HolderPartyID: facts.HolderPartyID, OccurredOn: in.OccurredOn,
		Location: in.Location, ClaimType: in.ClaimType, Description: in.Description, Currency: facts.Currency,
	}
	if in.ClaimedAmount != nil {
		m, err := kernel.NewMoney(*in.ClaimedAmount, facts.Currency)
		if err != nil {
			return View{}, domain.ErrInvalidAmount
		}
		draft.ClaimedAmount = &m
	}
	now := s.Clock.Now().UTC()
	claim, err := domain.New(draft, policyView(facts), kernel.Today(s.Clock), now)
	if err != nil {
		return View{}, err
	}
	snap := claim.Snapshot()
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Claims().Insert(ctx, snap, now); err != nil {
			return err
		}
		if err := uow.Publish(ctx, contracts.TopicClaimReported, contracts.ClaimReported{ClaimID: id, PolicyID: in.PolicyID}); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("claims.claim.reported", id))
	})
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, snap)
}
