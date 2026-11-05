// Package crmhttp expone crm. Solo mapea HTTP ↔ casos de uso.
package crmhttp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/crm/app"
	"github.com/marvinrepremarca/seguros/internal/modules/crm/domain"
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

func toLead(s domain.Snapshot) Lead {
	out := Lead{Id: s.ID, Name: s.Name, Source: LeadSource(s.Source), Status: LeadStatus(s.Status), Version: s.Version, FollowUpOn: s.FollowUpOn, ContactedAt: s.ContactedAt}
	if s.Email != "" {
		out.Email = &s.Email
	}
	if s.Phone != "" {
		out.Phone = &s.Phone
	}
	if s.LostReason != "" {
		out.LostReason = &s.LostReason
	}
	if s.LineOfBusinessID.Valid {
		out.LineOfBusinessId = &s.LineOfBusinessID.UUID
	}
	if s.AgentID.Valid {
		out.AgentId = &s.AgentID.UUID
	}
	if s.PartyID.Valid {
		out.PartyId = &s.PartyID.UUID
	}
	return out
}

func nullID(p *uuid.UUID) uuid.NullUUID {
	if p == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *p, Valid: true}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ListLeads implementa StrictServerInterface.
func (s *Server) ListLeads(ctx context.Context, req ListLeadsRequestObject) (ListLeadsResponseObject, error) {
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
	out := ListLeads200JSONResponse{Items: make([]Lead, 0, len(rows))}
	if next != "" {
		out.NextCursor = &next
	}
	for _, r := range rows {
		out.Items = append(out.Items, toLead(r))
	}
	return out, nil
}

// CreateLead implementa StrictServerInterface.
func (s *Server) CreateLead(ctx context.Context, req CreateLeadRequestObject) (CreateLeadResponseObject, error) {
	v, err := s.svc.Create(ctx, app.CreateInput{
		Name: req.Body.Name, Email: deref(req.Body.Email), Phone: deref(req.Body.Phone), Source: string(req.Body.Source),
		LineOfBusinessID: nullID(req.Body.LineOfBusinessId), AgentID: nullID(req.Body.AgentId),
	})
	if err != nil {
		return nil, err
	}
	return CreateLead201JSONResponse(toLead(v)), nil
}

// GetLead implementa StrictServerInterface.
func (s *Server) GetLead(ctx context.Context, req GetLeadRequestObject) (GetLeadResponseObject, error) {
	v, err := s.svc.Get(ctx, req.LeadId)
	if err != nil {
		return nil, err
	}
	return GetLead200JSONResponse(toLead(v)), nil
}

// MarkLeadContacted implementa StrictServerInterface.
func (s *Server) MarkLeadContacted(ctx context.Context, req MarkLeadContactedRequestObject) (MarkLeadContactedResponseObject, error) {
	v, err := s.svc.MarkContacted(ctx, req.LeadId)
	if err != nil {
		return nil, err
	}
	return MarkLeadContacted200JSONResponse(toLead(v)), nil
}

// MarkLeadInterested implementa StrictServerInterface.
func (s *Server) MarkLeadInterested(ctx context.Context, req MarkLeadInterestedRequestObject) (MarkLeadInterestedResponseObject, error) {
	v, err := s.svc.MarkInterested(ctx, req.LeadId)
	if err != nil {
		return nil, err
	}
	return MarkLeadInterested200JSONResponse(toLead(v)), nil
}

// ScheduleLeadFollowUp implementa StrictServerInterface.
func (s *Server) ScheduleLeadFollowUp(ctx context.Context, req ScheduleLeadFollowUpRequestObject) (ScheduleLeadFollowUpResponseObject, error) {
	v, err := s.svc.ScheduleFollowUp(ctx, req.LeadId, req.Body.FollowUpOn)
	if err != nil {
		return nil, err
	}
	return ScheduleLeadFollowUp200JSONResponse(toLead(v)), nil
}

// MarkLeadLost implementa StrictServerInterface.
func (s *Server) MarkLeadLost(ctx context.Context, req MarkLeadLostRequestObject) (MarkLeadLostResponseObject, error) {
	v, err := s.svc.MarkLost(ctx, req.LeadId, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return MarkLeadLost200JSONResponse(toLead(v)), nil
}

// StartLeadQuoting implementa StrictServerInterface.
func (s *Server) StartLeadQuoting(ctx context.Context, req StartLeadQuotingRequestObject) (StartLeadQuotingResponseObject, error) {
	v, err := s.svc.StartQuoting(ctx, req.LeadId, req.Body.PartyId)
	if err != nil {
		return nil, err
	}
	return StartLeadQuoting200JSONResponse(toLead(v)), nil
}

// AssignLead implementa StrictServerInterface.
func (s *Server) AssignLead(ctx context.Context, req AssignLeadRequestObject) (AssignLeadResponseObject, error) {
	v, err := s.svc.Assign(ctx, req.LeadId, req.Body.AgentId)
	if err != nil {
		return nil, err
	}
	return AssignLead200JSONResponse(toLead(v)), nil
}
