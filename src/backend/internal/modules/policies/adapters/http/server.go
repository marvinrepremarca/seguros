// Package policieshttp expone la póliza y su ciclo de vida. Solo mapea HTTP ↔ casos de uso.
package policieshttp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/app"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Server implementa StrictServerInterface.
type Server struct {
	svc    *app.Service
	policy *authz.Policy
	logger *slog.Logger
}

var _ StrictServerInterface = (*Server)(nil)

// NewServer construye el servidor.
func NewServer(svc *app.Service, policy *authz.Policy, logger *slog.Logger) *Server {
	return &Server{svc: svc, policy: policy, logger: logger}
}

// Mount registra las rutas con el contrato (autorización por x-permission + validación).
func (s *Server) Mount(r chi.Router) error {
	spec, err := GetSpec()
	if err != nil {
		return err
	}
	guard, err := authz.NewOperationGuard(spec, s.policy)
	if err != nil {
		return err
	}
	contract, err := httpx.Contract(spec, guard.Check, s.logger)
	if err != nil {
		return err
	}
	strictGuard := func(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			if err := guard.Check(ctx, operationID); err != nil {
				return nil, err
			}
			return f(ctx, w, r, req)
		}
	}
	h := NewStrictHandlerWithOptions(s, []StrictMiddlewareFunc{strictGuard}, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			httpx.WriteProblem(w, r, http.StatusBadRequest, httpx.CodeBadRequest, "La solicitud no es válida")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) { httpx.WriteError(w, r, s.logger, err) },
	})
	r.Group(func(g chi.Router) {
		g.Use(contract)
		HandlerWithOptions(h, ChiServerOptions{BaseRouter: g})
	})
	return nil
}

func nullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toParties(in []PolicyPartyInput) []domain.PolicyParty {
	out := make([]domain.PolicyParty, 0, len(in))
	for _, p := range in {
		out = append(out, domain.PolicyParty{PartyID: p.PartyId, Role: domain.PartyRole(p.Role)})
	}
	return out
}

func toTerms(b PolicyTerms) (app.TermsInput, error) {
	in := app.TermsInput{
		InsurerID: b.InsurerId, LineOfBusinessID: b.LineOfBusinessId, ProductID: nullUUID(b.ProductId), AgentID: nullUUID(b.AgentId),
		Currency: kernel.Currency(b.Currency), NetPremium: b.NetPremium, Taxes: b.Taxes, EffectiveFrom: b.EffectiveFrom, EffectiveTo: b.EffectiveTo,
		PaymentForm: domain.PaymentForm(b.PaymentForm), Parties: toParties(b.Parties),
	}
	for _, c := range b.Coverages {
		deductible := ""
		if c.Deductible != nil {
			deductible = *c.Deductible
		}
		in.Coverages = append(in.Coverages, app.CoverageInput{Name: c.Name, SumInsured: c.SumInsured, Deductible: deductible})
	}
	for _, i := range b.Items {
		data, err := json.Marshal(i.RiskData)
		if err != nil {
			return app.TermsInput{}, fmt.Errorf("policies: datos de riesgo: %w", err)
		}
		in.Items = append(in.Items, app.ItemInput{Description: i.Description, RiskData: data})
	}
	return in, nil
}

func toPolicy(v app.View) (Policy, error) {
	t := v.Terms
	total, err := v.TotalPremium()
	if err != nil {
		return Policy{}, fmt.Errorf("policies: total de prima: %w", err)
	}
	out := Policy{
		Id: uuid.UUID(v.ID), Status: PolicyStatus(v.Status), PolicyNumber: optional(v.Number), InsurerId: t.InsurerID, LineOfBusinessId: t.LineOfBusinessID,
		Currency: Currency(t.NetPremium.Currency()), NetPremium: t.NetPremium.Amount(), Taxes: t.Taxes.Amount(), TotalPremium: total.Amount(),
		EffectiveFrom: t.Period.From(), EffectiveTo: t.Period.To(), PaymentForm: PaymentForm(t.PaymentForm), IssuedAt: v.IssuedAt,
		RejectionReason: optional(v.RejectionReason), Version: v.Version, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
		Parties: make([]PolicyParty, 0, len(t.Parties)), Coverages: make([]Coverage, 0, len(t.Coverages)),
		Items: make([]InsuredItem, 0, len(t.Items)), Endorsements: make([]Endorsement, 0, len(v.Endorsements)),
	}
	if t.ProductID.Valid {
		out.ProductId = &t.ProductID.UUID
	}
	if t.AgentID.Valid {
		out.AgentId = &t.AgentID.UUID
	}
	if v.RenewedFrom.Valid {
		out.RenewedFromPolicyId = &v.RenewedFrom.UUID
	}
	if c := v.Cancellation; c != nil {
		out.Cancellation = &Cancellation{
			Cause: CancellationCause(c.Cause), EffectiveDate: c.EffectiveDate, UnearnedPremium: c.UnearnedPremium.Amount(), InsurerReference: optional(c.InsurerReference),
		}
	}
	for _, p := range t.Parties {
		out.Parties = append(out.Parties, PolicyParty{PartyId: p.PartyID, Role: PartyRole(p.Role), Name: v.PartyNames[p.PartyID]})
	}
	for _, c := range t.Coverages {
		out.Coverages = append(out.Coverages, Coverage{Id: c.ID, Name: c.Name, SumInsured: c.SumInsured.Amount(), Deductible: optional(c.Deductible)})
	}
	for _, i := range t.Items {
		var data map[string]interface{}
		if err := json.Unmarshal(i.RiskData, &data); err != nil {
			return Policy{}, fmt.Errorf("policies: datos de riesgo guardados: %w", err)
		}
		out.Items = append(out.Items, InsuredItem{Id: i.ID, Description: i.Description, RiskSchemaKey: i.RiskSchemaKey, RiskSchemaVersion: i.RiskSchemaVersion, RiskData: data})
	}
	for _, e := range v.Endorsements {
		item := Endorsement{
			Id: e.ID, Type: EndorsementType(e.Type), Status: EndorsementStatus(e.Status), Description: e.Description, EffectiveDate: e.EffectiveDate,
			RequestedAt: e.RequestedAt, ResolvedAt: e.ResolvedAt, Number: optional(e.Number), RejectionReason: optional(e.RejectionReason),
		}
		if e.NetPremiumDelta != nil {
			d := e.NetPremiumDelta.Amount()
			item.NetPremiumDelta = &d
		}
		if e.TaxesDelta != nil {
			d := e.TaxesDelta.Amount()
			item.TaxesDelta = &d
		}
		out.Endorsements = append(out.Endorsements, item)
	}
	return out, nil
}

// ListPolicies implementa StrictServerInterface.
func (s *Server) ListPolicies(ctx context.Context, req ListPoliciesRequestObject) (ListPoliciesResponseObject, error) {
	q := app.ListQuery{}
	if req.Params.Status != nil {
		q.Status = domain.Status(*req.Params.Status)
	}
	if req.Params.Cursor != nil {
		q.Cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		q.Limit = *req.Params.Limit
	}
	page, err := s.svc.List(ctx, q)
	if err != nil {
		return nil, err
	}
	items := make([]PolicySummary, 0, len(page.Items))
	for _, r := range page.Items {
		items = append(items, PolicySummary{
			Id: uuid.UUID(r.ID), Status: PolicyStatus(r.Status), PolicyNumber: optional(r.Number), InsurerId: r.InsurerID, LineOfBusinessId: r.LineOfBusinessID,
			HolderName: r.HolderName, Currency: Currency(r.NetPremium.Currency()), NetPremium: r.NetPremium.Amount(), Taxes: r.Taxes.Amount(),
			EffectiveFrom: r.EffectiveFrom, EffectiveTo: r.EffectiveTo,
		})
	}
	return ListPolicies200JSONResponse{Items: items, NextCursor: optional(page.NextCursor)}, nil
}

// ListAgents implementa StrictServerInterface.
func (s *Server) ListAgents(ctx context.Context, _ ListAgentsRequestObject) (ListAgentsResponseObject, error) {
	users, err := s.svc.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Agent, 0, len(users))
	for _, u := range users {
		items = append(items, Agent{Id: u.ID, DisplayName: u.DisplayName, Active: u.Active})
	}
	return ListAgents200JSONResponse{Items: items}, nil
}

// respond convierte el resultado de un comando en la respuesta del contrato.
func respond[R any](v app.View, err error, wrap func(PolicyJSONResponse) R) (R, error) {
	var zero R
	if err != nil {
		return zero, err
	}
	p, err := toPolicy(v)
	if err != nil {
		return zero, err
	}
	return wrap(PolicyJSONResponse(p)), nil
}

// CreatePolicy implementa StrictServerInterface.
func (s *Server) CreatePolicy(ctx context.Context, req CreatePolicyRequestObject) (CreatePolicyResponseObject, error) {
	in, err := toTerms(*req.Body)
	if err != nil {
		return nil, err
	}
	v, err := s.svc.Create(ctx, in)
	return respond[CreatePolicyResponseObject](v, err, func(p PolicyJSONResponse) CreatePolicyResponseObject { return CreatePolicy201JSONResponse{p} })
}

// GetPolicy implementa StrictServerInterface.
func (s *Server) GetPolicy(ctx context.Context, req GetPolicyRequestObject) (GetPolicyResponseObject, error) {
	v, err := s.svc.Get(ctx, domain.PolicyID(req.PolicyId))
	return respond[GetPolicyResponseObject](v, err, func(p PolicyJSONResponse) GetPolicyResponseObject { return GetPolicy200JSONResponse{p} })
}

// UpdatePolicyTerms implementa StrictServerInterface.
func (s *Server) UpdatePolicyTerms(ctx context.Context, req UpdatePolicyTermsRequestObject) (UpdatePolicyTermsResponseObject, error) {
	in, err := toTerms(*req.Body)
	if err != nil {
		return nil, err
	}
	v, err := s.svc.UpdateTerms(ctx, domain.PolicyID(req.PolicyId), req.Params.Version, in)
	return respond[UpdatePolicyTermsResponseObject](v, err, func(p PolicyJSONResponse) UpdatePolicyTermsResponseObject { return UpdatePolicyTerms200JSONResponse{p} })
}

// AcceptPolicy implementa StrictServerInterface.
func (s *Server) AcceptPolicy(ctx context.Context, req AcceptPolicyRequestObject) (AcceptPolicyResponseObject, error) {
	v, err := s.svc.Accept(ctx, domain.PolicyID(req.PolicyId), req.Params.Version)
	return respond[AcceptPolicyResponseObject](v, err, func(p PolicyJSONResponse) AcceptPolicyResponseObject { return AcceptPolicy200JSONResponse{p} })
}

// IssuePolicy implementa StrictServerInterface.
func (s *Server) IssuePolicy(ctx context.Context, req IssuePolicyRequestObject) (IssuePolicyResponseObject, error) {
	v, err := s.svc.Issue(ctx, domain.PolicyID(req.PolicyId), req.Params.Version, req.Body.PolicyNumber)
	return respond[IssuePolicyResponseObject](v, err, func(p PolicyJSONResponse) IssuePolicyResponseObject { return IssuePolicy200JSONResponse{p} })
}

// ReassignPolicyAgent implementa StrictServerInterface.
func (s *Server) ReassignPolicyAgent(ctx context.Context, req ReassignPolicyAgentRequestObject) (ReassignPolicyAgentResponseObject, error) {
	v, err := s.svc.ReassignAgent(ctx, domain.PolicyID(req.PolicyId), req.Params.Version, req.Body.AgentId, string(req.Body.Reason), deref(req.Body.Note))
	return respond[ReassignPolicyAgentResponseObject](v, err, func(p PolicyJSONResponse) ReassignPolicyAgentResponseObject {
		return ReassignPolicyAgent200JSONResponse{p}
	})
}

// TransferPortfolio implementa StrictServerInterface.
func (s *Server) TransferPortfolio(ctx context.Context, req TransferPortfolioRequestObject) (TransferPortfolioResponseObject, error) {
	if err := s.svc.TransferPortfolio(ctx, req.Body.FromAgentId, req.Body.ToAgentId, string(req.Body.Reason), deref(req.Body.Note)); err != nil {
		return nil, err
	}
	return TransferPortfolio202Response{}, nil
}

// RejectPolicy implementa StrictServerInterface.
func (s *Server) RejectPolicy(ctx context.Context, req RejectPolicyRequestObject) (RejectPolicyResponseObject, error) {
	v, err := s.svc.Reject(ctx, domain.PolicyID(req.PolicyId), req.Params.Version, req.Body.Reason)
	return respond[RejectPolicyResponseObject](v, err, func(p PolicyJSONResponse) RejectPolicyResponseObject { return RejectPolicy200JSONResponse{p} })
}

// CancelPolicy implementa StrictServerInterface.
func (s *Server) CancelPolicy(ctx context.Context, req CancelPolicyRequestObject) (CancelPolicyResponseObject, error) {
	b := req.Body
	reference := ""
	if b.InsurerReference != nil {
		reference = *b.InsurerReference
	}
	v, err := s.svc.Cancel(ctx, app.CancelCommand{
		ID: domain.PolicyID(req.PolicyId), Version: req.Params.Version, Cause: domain.CancellationCause(b.Cause),
		EffectiveDate: b.EffectiveDate, UnearnedPremium: b.UnearnedPremium, InsurerReference: reference,
	})
	return respond[CancelPolicyResponseObject](v, err, func(p PolicyJSONResponse) CancelPolicyResponseObject { return CancelPolicy200JSONResponse{p} })
}

// RequestEndorsement implementa StrictServerInterface.
func (s *Server) RequestEndorsement(ctx context.Context, req RequestEndorsementRequestObject) (RequestEndorsementResponseObject, error) {
	b := req.Body
	v, err := s.svc.RequestEndorsement(ctx, domain.PolicyID(req.PolicyId), req.Params.Version, domain.EndorsementType(b.Type), b.Description, b.EffectiveDate)
	return respond[RequestEndorsementResponseObject](v, err, func(p PolicyJSONResponse) RequestEndorsementResponseObject {
		return RequestEndorsement201JSONResponse{p}
	})
}

// IssueEndorsement implementa StrictServerInterface.
func (s *Server) IssueEndorsement(ctx context.Context, req IssueEndorsementRequestObject) (IssueEndorsementResponseObject, error) {
	b := req.Body
	cmd := app.IssueEndorsementCommand{
		PolicyID: domain.PolicyID(req.PolicyId), Version: req.Params.Version, EndorsementID: req.EndorsementId,
		Number: b.Number, NetPremiumDelta: b.NetPremiumDelta, TaxesDelta: b.TaxesDelta,
	}
	if b.CoverageChanges != nil {
		for _, c := range *b.CoverageChanges {
			cmd.CoverageChanges = append(cmd.CoverageChanges, app.CoverageChangeInput{CoverageID: c.CoverageId, SumInsured: c.SumInsured})
		}
	}
	if b.AddParties != nil {
		cmd.AddParties = toParties(*b.AddParties)
	}
	if b.RemoveParties != nil {
		cmd.RemoveParties = toParties(*b.RemoveParties)
	}
	if b.ItemChanges != nil {
		for _, c := range *b.ItemChanges {
			data, err := json.Marshal(c.RiskData)
			if err != nil {
				return nil, fmt.Errorf("policies: datos de riesgo: %w", err)
			}
			cmd.ItemChanges = append(cmd.ItemChanges, domain.ItemChange{ItemID: c.ItemId, RiskData: data})
		}
	}
	v, err := s.svc.IssueEndorsement(ctx, cmd)
	return respond[IssueEndorsementResponseObject](v, err, func(p PolicyJSONResponse) IssueEndorsementResponseObject { return IssueEndorsement200JSONResponse{p} })
}

// RejectEndorsement implementa StrictServerInterface.
func (s *Server) RejectEndorsement(ctx context.Context, req RejectEndorsementRequestObject) (RejectEndorsementResponseObject, error) {
	v, err := s.svc.RejectEndorsement(ctx, domain.PolicyID(req.PolicyId), req.Params.Version, req.EndorsementId, req.Body.Reason)
	return respond[RejectEndorsementResponseObject](v, err, func(p PolicyJSONResponse) RejectEndorsementResponseObject { return RejectEndorsement200JSONResponse{p} })
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
