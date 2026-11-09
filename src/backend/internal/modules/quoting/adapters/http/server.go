// Package quotinghttp expone cotización. Solo mapea HTTP ↔ casos de uso.
package quotinghttp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/quoting/app"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Server implementa StrictServerInterface.
type Server struct {
	svc    *app.Service
	policy *authz.Policy
	logger *slog.Logger
	clock  kernel.Clock
}

var _ StrictServerInterface = (*Server)(nil)

// NewServer construye el servidor.
func NewServer(svc *app.Service, policy *authz.Policy, logger *slog.Logger, clock kernel.Clock) *Server {
	return &Server{svc: svc, policy: policy, logger: logger, clock: clock}
}

// Mount registra las rutas con el contrato.
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
		HandlerWithOptions(h, ChiServerOptions{BaseRouter: g, ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			httpx.WriteProblem(w, r, http.StatusBadRequest, httpx.CodeBadRequest, "La solicitud no es válida")
		}})
	})
	return nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func nullID(p *uuid.UUID) uuid.NullUUID {
	if p == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *p, Valid: true}
}

func (s *Server) toRequest(v app.View) (QuoteRequest, error) {
	var risk map[string]any
	if err := json.Unmarshal(v.RiskData, &risk); err != nil {
		return QuoteRequest{}, fmt.Errorf("quoting: riesgo guardado: %w", err)
	}
	today := kernel.Today(s.clock)
	out := QuoteRequest{
		Id: v.ID, HolderPartyId: v.HolderPartyID, HolderName: v.HolderName, LineOfBusinessId: v.LineOfBusinessID, AgentId: v.AgentID, RiskDescription: v.RiskDescription,
		RiskData: risk, Status: RequestStatus(v.Status), Version: v.Version, Offers: make([]QuoteOffer, 0, len(v.Offers)),
	}
	if v.LeadID.Valid {
		out.LeadId = &v.LeadID.UUID
	}
	if v.PolicyID.Valid {
		out.PolicyId = &v.PolicyID.UUID
	}
	if v.DeclineReason != "" {
		out.DeclineReason = &v.DeclineReason
	}
	for _, o := range v.Offers {
		offer := QuoteOffer{
			Id: o.ID, InsurerId: o.InsurerID, Currency: string(o.NetPremium.Currency()), NetPremium: o.NetPremium.Amount(), Taxes: o.Taxes.Amount(), EffectiveFrom: o.EffectiveFrom, EffectiveTo: o.EffectiveTo,
			ValidUntil: o.ValidUntil, Status: OfferStatus(o.Status), Expired: today.After(o.ValidUntil), Coverages: make([]Coverage, 0, len(o.Coverages)),
		}
		if o.ProductID.Valid {
			id := o.ProductID.UUID
			offer.ProductId = &id
		}
		if o.Reference != "" {
			ref := o.Reference
			offer.Reference = &ref
		}
		for _, c := range o.Coverages {
			cov := Coverage{Name: c.Name, SumInsured: c.SumInsured.Amount()}
			if c.Deductible != "" {
				d := c.Deductible
				cov.Deductible = &d
			}
			offer.Coverages = append(offer.Coverages, cov)
		}
		out.Offers = append(out.Offers, offer)
	}
	return out, nil
}

// ListQuoteRequests implementa StrictServerInterface.
func (s *Server) ListQuoteRequests(ctx context.Context, req ListQuoteRequestsRequestObject) (ListQuoteRequestsResponseObject, error) {
	var status string
	var limit int32
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	rows, next, err := s.svc.List(ctx, status, deref(req.Params.Cursor), limit)
	if err != nil {
		return nil, err
	}
	out := ListQuoteRequests200JSONResponse{Items: make([]QuoteRequest, 0, len(rows))}
	if next != "" {
		out.NextCursor = &next
	}
	for _, r := range rows {
		item, err := s.toRequest(r)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// CreateQuoteRequest implementa StrictServerInterface.
func (s *Server) CreateQuoteRequest(ctx context.Context, req CreateQuoteRequestRequestObject) (CreateQuoteRequestResponseObject, error) {
	risk, err := json.Marshal(req.Body.RiskData)
	if err != nil {
		return nil, fmt.Errorf("quoting: %w", err)
	}
	v, err := s.svc.Create(ctx, app.CreateInput{LeadID: nullID(req.Body.LeadId), HolderPartyID: req.Body.HolderPartyId, LineOfBusinessID: req.Body.LineOfBusinessId, RiskDescription: req.Body.RiskDescription, RiskData: risk})
	if err != nil {
		return nil, err
	}
	out, err := s.toRequest(v)
	if err != nil {
		return nil, err
	}
	return CreateQuoteRequest201JSONResponse(out), nil
}

// GetQuoteRequest implementa StrictServerInterface.
func (s *Server) GetQuoteRequest(ctx context.Context, req GetQuoteRequestRequestObject) (GetQuoteRequestResponseObject, error) {
	v, err := s.svc.Get(ctx, req.RequestId)
	if err != nil {
		return nil, err
	}
	out, err := s.toRequest(v)
	if err != nil {
		return nil, err
	}
	return GetQuoteRequest200JSONResponse(out), nil
}

// AddQuoteOffer implementa StrictServerInterface.
func (s *Server) AddQuoteOffer(ctx context.Context, req AddQuoteOfferRequestObject) (AddQuoteOfferResponseObject, error) {
	b := req.Body
	in := app.OfferInput{
		InsurerID: b.InsurerId, ProductID: nullID(b.ProductId), Reference: deref(b.Reference), Currency: kernel.Currency(b.Currency), NetPremium: b.NetPremium, Taxes: b.Taxes,
		EffectiveFrom: b.EffectiveFrom, EffectiveTo: b.EffectiveTo,
	}
	if b.ValidUntil != nil {
		in.ValidUntil = *b.ValidUntil
	}
	for _, c := range b.Coverages {
		in.Coverages = append(in.Coverages, app.CoverageInput{Name: c.Name, SumInsured: c.SumInsured, Deductible: deref(c.Deductible)})
	}
	v, err := s.svc.AddOffer(ctx, req.RequestId, in)
	if err != nil {
		return nil, err
	}
	out, err := s.toRequest(v)
	if err != nil {
		return nil, err
	}
	return AddQuoteOffer200JSONResponse(out), nil
}

// AcceptQuoteOffer implementa StrictServerInterface.
func (s *Server) AcceptQuoteOffer(ctx context.Context, req AcceptQuoteOfferRequestObject) (AcceptQuoteOfferResponseObject, error) {
	in := app.AcceptInput{OfferID: req.Body.OfferId, PaymentForm: string(req.Body.PaymentForm)}
	if req.Body.InsuredPartyIds != nil {
		in.Insured = *req.Body.InsuredPartyIds
	}
	v, err := s.svc.Accept(ctx, req.RequestId, in)
	if err != nil {
		return nil, err
	}
	out, err := s.toRequest(v)
	if err != nil {
		return nil, err
	}
	return AcceptQuoteOffer200JSONResponse(out), nil
}

// DeclineQuoteRequest implementa StrictServerInterface.
func (s *Server) DeclineQuoteRequest(ctx context.Context, req DeclineQuoteRequestRequestObject) (DeclineQuoteRequestResponseObject, error) {
	v, err := s.svc.Decline(ctx, req.RequestId, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	out, err := s.toRequest(v)
	if err != nil {
		return nil, err
	}
	return DeclineQuoteRequest200JSONResponse(out), nil
}

// GetQuotingSettings implementa StrictServerInterface.
func (s *Server) GetQuotingSettings(ctx context.Context, _ GetQuotingSettingsRequestObject) (GetQuotingSettingsResponseObject, error) {
	days, err := s.svc.OfferValidDays(ctx)
	if err != nil {
		return nil, err
	}
	return GetQuotingSettings200JSONResponse{OfferValidDays: days}, nil
}

// SetQuotingSettings implementa StrictServerInterface.
func (s *Server) SetQuotingSettings(ctx context.Context, req SetQuotingSettingsRequestObject) (SetQuotingSettingsResponseObject, error) {
	if err := s.svc.SetOfferValidDays(ctx, req.Body.OfferValidDays); err != nil {
		return nil, err
	}
	return SetQuotingSettings204Response{}, nil
}
