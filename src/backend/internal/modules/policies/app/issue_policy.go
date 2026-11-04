package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
)

// Issue registra la expedición. El KYC se decide antes de abrir la transacción (ADR 0012); un bloqueo
// por KYC queda en la bitácora como intento denegado.
func (s *Service) Issue(ctx context.Context, id domain.PolicyID, version int32, number string) (View, error) {
	if err := authz.Require(ctx, authz.PolicyManage); err != nil {
		return View{}, err
	}
	var holder uuid.UUID
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := uow.Policies().Get(ctx, id)
		if err != nil {
			return err
		}
		holder = p.HolderID()
		return nil
	})
	if err != nil {
		return View{}, err
	}
	clearance, source, err := s.Gate.Clearance(ctx, holder)
	if err != nil {
		return View{}, err
	}
	view, err := s.change(ctx, id, version, "policies.policy.issued", func(p *domain.Policy) (map[string]string, error) {
		if p.HolderID() != holder { // el tomador cambió desde que se evaluó el KYC
			return nil, domain.ErrConcurrentUpdate
		}
		return map[string]string{"kyc": kycLabel(clearance), "complianceFlagSource": string(source)}, p.Issue(number, clearance, s.Clock.Now())
	})
	if errors.Is(err, domain.ErrKycNotApproved) {
		s.auditKycDenied(ctx, id, source)
	}
	return view, err
}

func kycLabel(c domain.KycClearance) string {
	switch c {
	case domain.KycNotRequired:
		return "not_required"
	case domain.KycApproved:
		return "approved"
	case domain.KycNotApproved:
		return "not_approved"
	case domain.KycUndetermined:
		return "undetermined"
	default:
		return "undetermined"
	}
}

// auditKycDenied deja rastro del intento bloqueado. El bloqueo ya ocurrió: si esta escritura falla, se
// registra en el log y no cambia la respuesta.
func (s *Service) auditKycDenied(ctx context.Context, id domain.PolicyID, source flags.Source) {
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		return uow.Audit(ctx, audit.Entry{
			Action: "policies.policy.issue_denied", ResourceType: "policies.policy", ResourceID: id.String(), Outcome: audit.OutcomeDenied,
			Details: map[string]string{"kyc": "not_approved", "complianceFlagSource": string(source)},
		})
	})
	if err != nil {
		s.Logger.ErrorContext(ctx, "auditando expedición denegada", "error", err)
	}
}
