package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de la renovación.
var (
	ErrNotRenewable  = kernel.NewError(kernel.KindConflict, "policies.renewal.not_renewable", "solo se renueva una póliza vigente, en mora o vencida")
	ErrRenewalExist  = kernel.NewError(kernel.KindConflict, "policies.renewal.already_exists", "la póliza ya tiene una renovación en curso")
	ErrRenewalHolder = kernel.NewError(kernel.KindValidation, "policies.renewal.holder_changed", "una renovación conserva el tomador; con otro tomador es una póliza nueva")
	ErrRenewalLine   = kernel.NewError(kernel.KindValidation, "policies.renewal.line_changed", "una renovación conserva el ramo")
	ErrRenewalDates  = kernel.NewError(kernel.KindValidation, "policies.renewal.overlaps_previous", "la nueva vigencia debe empezar después de la anterior")
)

// Renew registra la nueva vigencia de una póliza en cotización; sigue el trámite normal hasta expedirse.
// La anterior no cambia: al vencer pasa a Renovada (DECISIONS N17).
func (s *Service) Renew(ctx context.Context, previous domain.PolicyID, in TermsInput) (View, error) {
	if err := authz.Require(ctx, authz.PolicyManage); err != nil {
		return View{}, err
	}
	terms, err := s.buildTerms(ctx, in)
	if err != nil {
		return View{}, err
	}
	id, err := kernel.NewID[domain.PolicyID]()
	if err != nil {
		return View{}, fmt.Errorf("policies: %w", err)
	}
	policy, err := domain.NewPolicy(id, terms, s.Clock.Now())
	if err != nil {
		return View{}, err
	}
	if err := policy.SetRenewedFrom(previous); err != nil {
		return View{}, err
	}
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		old, err := uow.Policies().Get(ctx, previous)
		if err != nil {
			return err
		}
		snap := old.Snapshot()
		switch snap.Status { //nolint:exhaustive // los demás estados no se renuevan
		case domain.StatusActive, domain.StatusInArrears, domain.StatusExpired:
		default:
			return ErrNotRenewable
		}
		if !terms.Period.From().After(snap.Terms.Period.To()) {
			return ErrRenewalDates
		}
		rules, err := uow.Policies().RenewalSettings(ctx)
		if err != nil {
			return err
		}
		if rules.KeepHolder && holderOf(terms) != holderOf(snap.Terms) {
			return ErrRenewalHolder
		}
		if rules.KeepLine && terms.LineOfBusinessID != snap.Terms.LineOfBusinessID {
			return ErrRenewalLine
		}
		existing, err := uow.Policies().RenewalsOf(ctx, previous)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			return ErrRenewalExist
		}
		if err := uow.Policies().Insert(ctx, policy); err != nil {
			return err
		}
		return record(ctx, uow, policy, "policies.policy.renewal_created", "", map[string]string{"renewed_from": uuid.UUID(previous).String()})
	})
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, policy.Snapshot())
}

// issuedRenewal devuelve la nueva vigencia ya expedida de la póliza, si existe.
func issuedRenewal(ctx context.Context, uow UnitOfWork, previous domain.PolicyID) (*domain.PolicyID, error) {
	rows, err := uow.Policies().RenewalsOf(ctx, previous)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.Status != domain.StatusInQuotation && r.Status != domain.StatusInProcess {
			id := r.ID
			return &id, nil
		}
	}
	return nil, nil //nolint:nilnil // sin renovación expedida no hay nada que devolver
}

// RenewPredecessor pasa a Renovada la póliza anterior si ya venció; si aún no, lo hará al vencer.
func RenewPredecessor(ctx context.Context, uow UnitOfWork, clock kernel.Clock, previous, renewedBy uuid.UUID) error {
	p, err := uow.Policies().Get(ctx, domain.PolicyID(previous))
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	from := p.Status()
	if from != domain.StatusExpired {
		return nil
	}
	if err := p.MarkRenewed(domain.PolicyID(renewedBy), clock.Now()); err != nil {
		return err
	}
	if err := uow.Policies().Save(ctx, p); err != nil {
		return err
	}
	return record(ctx, uow, p, "policies.policy.renewed", from, nil)
}

// MarkNotRenewed cierra la póliza vencida cuyo cliente no renueva. Si aún no vence, no aplica:
// Renewals repite el evento cuando venza.
func MarkNotRenewed(ctx context.Context, uow UnitOfWork, clock kernel.Clock, policyID uuid.UUID) error {
	p, err := uow.Policies().Get(ctx, domain.PolicyID(policyID))
	if err != nil {
		return err
	}
	from := p.Status()
	if from != domain.StatusExpired {
		return nil
	}
	if err := p.MarkNotRenewed(clock.Now()); err != nil {
		return err
	}
	if err := uow.Policies().Save(ctx, p); err != nil {
		return err
	}
	return record(ctx, uow, p, "policies.policy.not_renewed", from, nil)
}

func holderOf(t domain.Terms) uuid.UUID {
	for _, x := range t.Parties {
		if x.Role == domain.RoleHolder {
			return x.PartyID
		}
	}
	return uuid.Nil
}
