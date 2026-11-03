package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	identity "github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Create registra una póliza en cotización (expedición manual).
func (s *Service) Create(ctx context.Context, in TermsInput) (View, error) {
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
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Policies().Insert(ctx, policy); err != nil {
			return err
		}
		return record(ctx, uow, policy, "policies.policy.created", "", nil)
	})
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, policy.Snapshot())
}

// UpdateTerms ajusta los datos antes de expedir.
func (s *Service) UpdateTerms(ctx context.Context, id domain.PolicyID, version int32, in TermsInput) (View, error) {
	if err := authz.Require(ctx, authz.PolicyManage); err != nil {
		return View{}, err
	}
	terms, err := s.buildTerms(ctx, in)
	if err != nil {
		return View{}, err
	}
	return s.change(ctx, id, version, "policies.policy.terms_updated", func(p *domain.Policy) (map[string]string, error) {
		return nil, p.UpdateTerms(terms, s.Clock.Now())
	})
}

// Accept registra que el cliente aceptó.
func (s *Service) Accept(ctx context.Context, id domain.PolicyID, version int32) (View, error) {
	if err := authz.Require(ctx, authz.PolicyManage); err != nil {
		return View{}, err
	}
	return s.change(ctx, id, version, "policies.policy.accepted", func(p *domain.Policy) (map[string]string, error) {
		return nil, p.Accept(s.Clock.Now())
	})
}

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

// Reject registra el rechazo de la aseguradora.
func (s *Service) Reject(ctx context.Context, id domain.PolicyID, version int32, reason string) (View, error) {
	if err := authz.Require(ctx, authz.PolicyManage); err != nil {
		return View{}, err
	}
	return s.change(ctx, id, version, "policies.policy.rejected", func(p *domain.Policy) (map[string]string, error) {
		return nil, p.Reject(reason, s.Clock.Now())
	})
}

// CancelCommand registra la cancelación expedida por la aseguradora.
type CancelCommand struct {
	ID               domain.PolicyID
	Version          int32
	Cause            domain.CancellationCause
	EffectiveDate    kernel.Date
	UnearnedPremium  decimal.Decimal
	InsurerReference string
}

// Cancel registra la cancelación. La moneda de la prima no devengada es la de la póliza.
func (s *Service) Cancel(ctx context.Context, cmd CancelCommand) (View, error) {
	if err := authz.Require(ctx, authz.PolicyCancel); err != nil {
		return View{}, err
	}
	return s.change(ctx, cmd.ID, cmd.Version, "policies.policy.cancelled", func(p *domain.Policy) (map[string]string, error) {
		unearned, err := kernel.NewMoney(cmd.UnearnedPremium, p.Snapshot().Terms.NetPremium.Currency())
		if err != nil {
			return nil, err
		}
		return map[string]string{"cause": string(cmd.Cause)}, p.Cancel(domain.Cancellation{
			Cause: cmd.Cause, EffectiveDate: cmd.EffectiveDate, UnearnedPremium: unearned, InsurerReference: cmd.InsurerReference,
		}, s.Clock.Now())
	})
}

// RequestEndorsement registra una solicitud de endoso.
func (s *Service) RequestEndorsement(ctx context.Context, id domain.PolicyID, version int32, kind domain.EndorsementType, description string, effective kernel.Date) (View, error) {
	if err := authz.Require(ctx, authz.EndorsementManage); err != nil {
		return View{}, err
	}
	endorsement, err := uuid.NewV7()
	if err != nil {
		return View{}, fmt.Errorf("policies: %w", err)
	}
	return s.change(ctx, id, version, "policies.endorsement.requested", func(p *domain.Policy) (map[string]string, error) {
		return map[string]string{"endorsementId": endorsement.String(), "type": string(kind)}, p.RequestEndorsement(endorsement, kind, description, effective, s.Clock.Now())
	})
}

// CoverageChangeInput cambia la suma asegurada de una cobertura.
type CoverageChangeInput struct {
	CoverageID uuid.UUID
	SumInsured decimal.Decimal
}

// IssueEndorsementCommand registra el endoso emitido por la aseguradora.
type IssueEndorsementCommand struct {
	PolicyID        domain.PolicyID
	Version         int32
	EndorsementID   uuid.UUID
	Number          string
	NetPremiumDelta decimal.Decimal
	TaxesDelta      decimal.Decimal
	CoverageChanges []CoverageChangeInput
	AddParties      []domain.PolicyParty
	RemoveParties   []domain.PolicyParty
	ItemChanges     []domain.ItemChange
}

// IssueEndorsement aplica el endoso. Las partes nuevas se validan contra Parties antes de la
// transacción; los datos de riesgo, contra el esquema con el que se guardó cada bien.
func (s *Service) IssueEndorsement(ctx context.Context, cmd IssueEndorsementCommand) (View, error) {
	if err := authz.Require(ctx, authz.EndorsementManage); err != nil {
		return View{}, err
	}
	if len(cmd.AddParties) > 0 {
		if err := s.checkParties(ctx, cmd.AddParties); err != nil {
			return View{}, err
		}
	}
	return s.change(ctx, cmd.PolicyID, cmd.Version, "policies.endorsement.issued", func(p *domain.Policy) (map[string]string, error) {
		snap := p.Snapshot()
		currency := snap.Terms.NetPremium.Currency()
		net, err := kernel.NewMoney(cmd.NetPremiumDelta, currency)
		if err != nil {
			return nil, err
		}
		taxes, err := kernel.NewMoney(cmd.TaxesDelta, currency)
		if err != nil {
			return nil, err
		}
		effects := domain.EndorsementEffects{AddParties: cmd.AddParties, RemoveParties: cmd.RemoveParties, ItemChanges: cmd.ItemChanges}
		for _, c := range cmd.CoverageChanges {
			sum, err := kernel.NewMoney(c.SumInsured, currency)
			if err != nil {
				return nil, err
			}
			effects.CoverageChanges = append(effects.CoverageChanges, domain.CoverageChange{CoverageID: c.CoverageID, SumInsured: sum})
		}
		for _, c := range cmd.ItemChanges {
			for _, item := range snap.Terms.Items {
				if item.ID != c.ItemID {
					continue
				}
				if err := s.Schemas.Validate(item.RiskSchemaKey, c.RiskData); err != nil {
					return nil, err
				}
			}
		}
		return map[string]string{
			"endorsementId": cmd.EndorsementID.String(), "netPremiumDelta": net.Amount().String(), "taxesDelta": taxes.Amount().String(),
		}, p.IssueEndorsement(cmd.EndorsementID, cmd.Number, net, taxes, effects, s.Clock.Now())
	})
}

// RejectEndorsement registra que la aseguradora no emitió el endoso.
func (s *Service) RejectEndorsement(ctx context.Context, id domain.PolicyID, version int32, endorsement uuid.UUID, reason string) (View, error) {
	if err := authz.Require(ctx, authz.EndorsementManage); err != nil {
		return View{}, err
	}
	return s.change(ctx, id, version, "policies.endorsement.rejected", func(p *domain.Policy) (map[string]string, error) {
		return map[string]string{"endorsementId": endorsement.String()}, p.RejectEndorsement(endorsement, reason, s.Clock.Now())
	})
}

// Get devuelve una póliza. Un asesor con read-own recibe 404 para las de otro: no revela que existen.
func (s *Service) Get(ctx context.Context, id domain.PolicyID) (View, error) {
	scope, err := readScope(ctx)
	if err != nil {
		return View{}, err
	}
	var snap domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := uow.Policies().Get(ctx, id)
		if err != nil {
			return err
		}
		snap = p.Snapshot()
		return nil
	})
	if err != nil {
		return View{}, err
	}
	if scope.Valid && snap.Terms.AgentID != scope {
		return View{}, domain.ErrNotFound
	}
	return s.view(ctx, snap)
}

// ListQuery filtra y pagina.
type ListQuery struct {
	Status domain.Status
	Cursor string
	Limit  int32
}

// Page es una página de pólizas.
type Page struct {
	Items      []Summary
	NextCursor string
}

// List lista las pólizas que el usuario puede ver, con el nombre del tomador.
func (s *Service) List(ctx context.Context, q ListQuery) (Page, error) {
	scope, err := readScope(ctx)
	if err != nil {
		return Page{}, err
	}
	before, err := kernel.DecodeCursor(q.Cursor)
	if err != nil {
		return Page{}, err
	}
	size := kernel.PageSize(q.Limit)
	var rows []Summary
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		rows, err = uow.Policies().List(ctx, ListFilter{Before: before, Agent: scope, Status: q.Status, Limit: size + 1})
		return err
	})
	if err != nil {
		return Page{}, err
	}
	var page Page
	if int32(len(rows)) > size { //nolint:gosec // acotado por MaxPageSize + 1
		rows = rows[:size]
		page.NextCursor = kernel.EncodeCursor(uuid.UUID(rows[size-1].ID))
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.HolderPartyID)
	}
	names, err := s.Parties.Summaries(ctx, ids)
	if err != nil {
		return Page{}, fmt.Errorf("policies: consultando tomadores: %w", err)
	}
	for i := range rows {
		rows[i].HolderName = names[rows[i].HolderPartyID].Name
	}
	page.Items = rows
	return page, nil
}

// ListAgents lista los usuarios que se pueden asignar como asesor.
func (s *Service) ListAgents(ctx context.Context) ([]identity.User, error) {
	if err := authz.Require(ctx, authz.PolicyManage); err != nil {
		return nil, err
	}
	users, err := s.Users.Users(ctx)
	if err != nil {
		return nil, fmt.Errorf("policies: consultando asesores: %w", err)
	}
	return users, nil
}
