// Package claimshttp expone siniestros. Solo mapea HTTP ↔ casos de uso.
package claimshttp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/marvinrepremarca/seguros/internal/modules/claims/app"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
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

func toClaim(v app.View) Claim {
	out := Claim{
		Id: v.ID, PolicyId: v.PolicyID, InsurerId: v.InsurerID, HolderPartyId: v.HolderPartyID, HolderName: v.HolderName, OccurredOn: v.OccurredOn, Location: v.Location, ClaimType: v.ClaimType,
		Description: v.Description, Currency: string(v.Currency), Status: ClaimStatus(v.Status), AlertOutOfCoverage: v.AlertOutOfCoverage, AlertInArrears: v.AlertInArrears, AlertNotInForce: v.AlertNotInForce,
		ReportedAt: v.ReportedAt, NotifiedAt: v.NotifiedAt, Version: v.Version,
	}
	if v.PolicyNumber != "" {
		out.PolicyNumber = &v.PolicyNumber
	}
	if v.ClaimedAmount != nil {
		a := v.ClaimedAmount.Amount()
		out.ClaimedAmount = &a
	}
	if v.PaidAmount != nil {
		a := v.PaidAmount.Amount()
		out.PaidAmount = &a
	}
	if v.InsurerClaimNumber != "" {
		out.InsurerClaimNumber = &v.InsurerClaimNumber
	}
	if v.DecisionSupport != "" {
		out.DecisionSupport = &v.DecisionSupport
	}
	if v.HasHealthDetails {
		has := true
		out.HasHealthDetails = &has
	}
	return out
}

// ListClaims implementa StrictServerInterface.
func (s *Server) ListClaims(ctx context.Context, req ListClaimsRequestObject) (ListClaimsResponseObject, error) {
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
	out := ListClaims200JSONResponse{Items: make([]Claim, 0, len(rows))}
	if next != "" {
		out.NextCursor = &next
	}
	for _, r := range rows {
		out.Items = append(out.Items, toClaim(r))
	}
	return out, nil
}

// ReportClaim implementa StrictServerInterface.
func (s *Server) ReportClaim(ctx context.Context, req ReportClaimRequestObject) (ReportClaimResponseObject, error) {
	b := req.Body
	v, err := s.svc.Report(ctx, app.ReportInput{PolicyID: b.PolicyId, OccurredOn: b.OccurredOn, Location: b.Location, ClaimType: b.ClaimType, Description: b.Description, ClaimedAmount: b.ClaimedAmount})
	if err != nil {
		return nil, err
	}
	return ReportClaim201JSONResponse(toClaim(v)), nil
}

// GetClaim implementa StrictServerInterface.
func (s *Server) GetClaim(ctx context.Context, req GetClaimRequestObject) (GetClaimResponseObject, error) {
	v, err := s.svc.Get(ctx, req.ClaimId)
	if err != nil {
		return nil, err
	}
	return GetClaim200JSONResponse(toClaim(v)), nil
}

// AdvanceClaim implementa StrictServerInterface.
func (s *Server) AdvanceClaim(ctx context.Context, req AdvanceClaimRequestObject) (AdvanceClaimResponseObject, error) {
	v, err := s.svc.Advance(ctx, req.ClaimId, app.AdvanceInput{To: string(req.Body.To), InsurerClaimNumber: deref(req.Body.InsurerClaimNumber), Support: deref(req.Body.Support)})
	if err != nil {
		return nil, err
	}
	return AdvanceClaim200JSONResponse(toClaim(v)), nil
}

// SetClaimedAmount implementa StrictServerInterface.
func (s *Server) SetClaimedAmount(ctx context.Context, req SetClaimedAmountRequestObject) (SetClaimedAmountResponseObject, error) {
	v, err := s.svc.SetClaimed(ctx, req.ClaimId, req.Body.Amount, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return SetClaimedAmount200JSONResponse(toClaim(v)), nil
}

// RecordClaimPayment implementa StrictServerInterface.
func (s *Server) RecordClaimPayment(ctx context.Context, req RecordClaimPaymentRequestObject) (RecordClaimPaymentResponseObject, error) {
	v, err := s.svc.RecordPayment(ctx, req.ClaimId, req.Body.Amount)
	if err != nil {
		return nil, err
	}
	return RecordClaimPayment200JSONResponse(toClaim(v)), nil
}

// OverrideClaimPayment implementa StrictServerInterface.
func (s *Server) OverrideClaimPayment(ctx context.Context, req OverrideClaimPaymentRequestObject) (OverrideClaimPaymentResponseObject, error) {
	v, err := s.svc.OverridePayment(ctx, req.ClaimId, req.Body.Amount, req.Body.Reason, req.Body.Support)
	if err != nil {
		return nil, err
	}
	return OverrideClaimPayment200JSONResponse(toClaim(v)), nil
}

// GetClaimHealth implementa StrictServerInterface.
func (s *Server) GetClaimHealth(ctx context.Context, req GetClaimHealthRequestObject) (GetClaimHealthResponseObject, error) {
	details, err := s.svc.Health(ctx, req.ClaimId)
	if err != nil {
		return nil, err
	}
	return GetClaimHealth200JSONResponse{Details: details}, nil
}

// SetClaimHealth implementa StrictServerInterface.
func (s *Server) SetClaimHealth(ctx context.Context, req SetClaimHealthRequestObject) (SetClaimHealthResponseObject, error) {
	if err := s.svc.SetHealth(ctx, req.ClaimId, req.Body.Details); err != nil {
		return nil, err
	}
	return SetClaimHealth204Response{}, nil
}

// GetClaimTracking implementa StrictServerInterface.
func (s *Server) GetClaimTracking(ctx context.Context, req GetClaimTrackingRequestObject) (GetClaimTrackingResponseObject, error) {
	items, notes, err := s.svc.Tracking(ctx, req.ClaimId)
	if err != nil {
		return nil, err
	}
	out := GetClaimTracking200JSONResponse{Checklist: make([]ChecklistItem, 0, len(items)), Notes: make([]ClaimNote, 0, len(notes))}
	for _, i := range items {
		out.Checklist = append(out.Checklist, ChecklistItem{Id: i.ID, Label: i.Label, Received: i.Received})
	}
	for _, n := range notes {
		out.Notes = append(out.Notes, ClaimNote{Id: n.ID, Body: n.Body, CreatedAt: n.CreatedAt})
	}
	return out, nil
}

// AddChecklistItem implementa StrictServerInterface.
func (s *Server) AddChecklistItem(ctx context.Context, req AddChecklistItemRequestObject) (AddChecklistItemResponseObject, error) {
	if err := s.svc.AddChecklistItem(ctx, req.ClaimId, req.Body.Label); err != nil {
		return nil, err
	}
	return AddChecklistItem204Response{}, nil
}

// SetChecklistItem implementa StrictServerInterface.
func (s *Server) SetChecklistItem(ctx context.Context, req SetChecklistItemRequestObject) (SetChecklistItemResponseObject, error) {
	if err := s.svc.SetChecklistItem(ctx, req.ClaimId, req.ItemId, req.Body.Received); err != nil {
		return nil, err
	}
	return SetChecklistItem204Response{}, nil
}

// AddClaimNote implementa StrictServerInterface.
func (s *Server) AddClaimNote(ctx context.Context, req AddClaimNoteRequestObject) (AddClaimNoteResponseObject, error) {
	if err := s.svc.AddNote(ctx, req.ClaimId, req.Body.Body); err != nil {
		return nil, err
	}
	return AddClaimNote204Response{}, nil
}
