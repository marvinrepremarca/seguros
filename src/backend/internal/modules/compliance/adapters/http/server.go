// Package compliancehttp expone cumplimiento. Solo mapea HTTP ↔ casos de uso.
package compliancehttp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/compliance/app"
	"github.com/marvinrepremarca/seguros/internal/modules/compliance/domain"
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

func lim(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

func toCheck(c domain.KycCheck) KycCheck {
	return KycCheck{Id: c.ID, PartyId: c.PartyID, Approved: c.Result == domain.ResultApproved, Source: c.Source, CheckedOn: c.CheckedOn, HasDetails: c.HasDetails}
}

func toComplaint(d domain.ComplaintData) Complaint {
	out := Complaint{Id: d.ID, Channel: d.Channel, Subject: d.Subject, Description: d.Description, Status: ComplaintStatus(d.Status), ReceivedOn: d.ReceivedOn, Version: d.Version}
	if d.PartyID.Valid {
		out.PartyId = &d.PartyID.UUID
	}
	if d.Resolution != "" {
		out.Resolution = &d.Resolution
	}
	return out
}

// GetKyc implementa StrictServerInterface.
func (s *Server) GetKyc(ctx context.Context, req GetKycRequestObject) (GetKycResponseObject, error) {
	st, err := s.svc.History(ctx, req.PartyId)
	if err != nil {
		return nil, err
	}
	out := GetKyc200JSONResponse{Approved: st.Approved, Stale: st.Stale, Checks: make([]KycCheck, 0, len(st.Checks))}
	for _, c := range st.Checks {
		out.Checks = append(out.Checks, toCheck(c))
	}
	return out, nil
}

// RecordKyc implementa StrictServerInterface.
func (s *Server) RecordKyc(ctx context.Context, req RecordKycRequestObject) (RecordKycResponseObject, error) {
	c, err := s.svc.Record(ctx, app.RecordInput{PartyID: req.PartyId, Approved: req.Body.Approved, Source: req.Body.Source, CheckedOn: req.Body.CheckedOn, Details: deref(req.Body.Details)})
	if err != nil {
		return nil, err
	}
	return RecordKyc201JSONResponse(toCheck(c)), nil
}

// ReadKycDetails implementa StrictServerInterface.
func (s *Server) ReadKycDetails(ctx context.Context, req ReadKycDetailsRequestObject) (ReadKycDetailsResponseObject, error) {
	d, err := s.svc.Details(ctx, req.PartyId, req.CheckId)
	if err != nil {
		return nil, err
	}
	return ReadKycDetails200JSONResponse{Details: d}, nil
}

// ExportHabeas implementa StrictServerInterface.
func (s *Server) ExportHabeas(ctx context.Context, req ExportHabeasRequestObject) (ExportHabeasResponseObject, error) {
	r, err := s.svc.Export(ctx, req.PartyId)
	if err != nil {
		return nil, err
	}
	out := ExportHabeas200JSONResponse{Kind: r.Kind, Name: r.Name, DocumentType: r.DocumentType, DocumentLast4: r.DocumentLast4, Consents: make([]HabeasConsent, 0, len(r.Consents))}
	if r.Email != "" {
		out.Email = &r.Email
	}
	if r.Phone != "" {
		out.Phone = &r.Phone
	}
	if r.Address != "" {
		out.Address = &r.Address
	}
	if r.City != "" {
		out.City = &r.City
	}
	for _, c := range r.Consents {
		out.Consents = append(out.Consents, HabeasConsent{Status: c.Status, Purpose: c.Purpose, TextVersion: c.TextVersion, ConsentDate: c.ConsentDate})
	}
	return out, nil
}

// RequestSuppression implementa StrictServerInterface.
func (s *Server) RequestSuppression(ctx context.Context, req RequestSuppressionRequestObject) (RequestSuppressionResponseObject, error) {
	id, err := s.svc.RequestSuppression(ctx, req.PartyId)
	if err != nil {
		return nil, err
	}
	return RequestSuppression201JSONResponse{Id: id}, nil
}

// ListSuppressions implementa StrictServerInterface.
func (s *Server) ListSuppressions(ctx context.Context, req ListSuppressionsRequestObject) (ListSuppressionsResponseObject, error) {
	rows, next, err := s.svc.ListSuppressions(ctx, deref(req.Params.Cursor), lim(req.Params.Limit))
	if err != nil {
		return nil, err
	}
	out := ListSuppressions200JSONResponse{Items: make([]Suppression, 0, len(rows))}
	if next != "" {
		out.NextCursor = &next
	}
	for _, r := range rows {
		item := Suppression{Id: r.ID, PartyId: r.PartyID, Status: SuppressionStatus(r.Status), CreatedAt: r.CreatedAt}
		if r.Resolution != "" {
			res := r.Resolution
			item.Resolution = &res
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// ResolveSuppression implementa StrictServerInterface.
func (s *Server) ResolveSuppression(ctx context.Context, req ResolveSuppressionRequestObject) (ResolveSuppressionResponseObject, error) {
	if err := s.svc.ResolveSuppression(ctx, req.RequestId, req.Body.Accepted, req.Body.Resolution); err != nil {
		return nil, err
	}
	return ResolveSuppression204Response{}, nil
}

// ListComplaints implementa StrictServerInterface.
func (s *Server) ListComplaints(ctx context.Context, req ListComplaintsRequestObject) (ListComplaintsResponseObject, error) {
	rows, next, err := s.svc.ListComplaints(ctx, deref(req.Params.Cursor), lim(req.Params.Limit))
	if err != nil {
		return nil, err
	}
	out := ListComplaints200JSONResponse{Items: make([]Complaint, 0, len(rows))}
	if next != "" {
		out.NextCursor = &next
	}
	for _, r := range rows {
		out.Items = append(out.Items, toComplaint(r))
	}
	return out, nil
}

// CreateComplaint implementa StrictServerInterface.
func (s *Server) CreateComplaint(ctx context.Context, req CreateComplaintRequestObject) (CreateComplaintResponseObject, error) {
	d := domain.ComplaintData{Channel: req.Body.Channel, Subject: req.Body.Subject, Description: req.Body.Description, ReceivedOn: req.Body.ReceivedOn}
	if req.Body.PartyId != nil {
		d.PartyID = uuid.NullUUID{UUID: *req.Body.PartyId, Valid: true}
	}
	v, err := s.svc.CreateComplaint(ctx, d)
	if err != nil {
		return nil, err
	}
	return CreateComplaint201JSONResponse(toComplaint(v)), nil
}

// StartComplaint implementa StrictServerInterface.
func (s *Server) StartComplaint(ctx context.Context, req StartComplaintRequestObject) (StartComplaintResponseObject, error) {
	v, err := s.svc.StartComplaint(ctx, req.ComplaintId)
	if err != nil {
		return nil, err
	}
	return StartComplaint200JSONResponse(toComplaint(v)), nil
}

// ResolveComplaint implementa StrictServerInterface.
func (s *Server) ResolveComplaint(ctx context.Context, req ResolveComplaintRequestObject) (ResolveComplaintResponseObject, error) {
	v, err := s.svc.ResolveComplaint(ctx, req.ComplaintId, req.Body.Resolution)
	if err != nil {
		return nil, err
	}
	return ResolveComplaint200JSONResponse(toComplaint(v)), nil
}
