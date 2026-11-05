// Package renewalshttp expone renovaciones. Solo mapea HTTP ↔ casos de uso.
package renewalshttp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/marvinrepremarca/seguros/internal/modules/renewals/app"
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

func toCase(v app.View) RenewalCase {
	out := RenewalCase{
		Id: v.ID, PolicyId: v.PolicyID, PolicyNumber: v.PolicyNumber, InsurerId: v.InsurerID, HolderPartyId: v.HolderPartyID, HolderName: v.HolderName,
		EffectiveTo: v.EffectiveTo, Status: CaseStatus(v.Status), Version: v.Version,
	}
	if v.LostReason != "" {
		reason := v.LostReason
		out.LostReason = &reason
	}
	if v.RenewedBy.Valid {
		id := v.RenewedBy.UUID
		out.RenewedByPolicyId = &id
	}
	return out
}

// ListCases implementa StrictServerInterface.
func (s *Server) ListCases(ctx context.Context, req ListCasesRequestObject) (ListCasesResponseObject, error) {
	var status, cursor string
	var limit int32
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	rows, next, err := s.svc.ListCases(ctx, status, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := ListCases200JSONResponse{Items: make([]RenewalCase, 0, len(rows))}
	if next != "" {
		out.NextCursor = &next
	}
	for _, v := range rows {
		out.Items = append(out.Items, toCase(v))
	}
	return out, nil
}

// StartCase implementa StrictServerInterface.
func (s *Server) StartCase(ctx context.Context, req StartCaseRequestObject) (StartCaseResponseObject, error) {
	v, err := s.svc.Start(ctx, req.PolicyId)
	if err != nil {
		return nil, err
	}
	return StartCase200JSONResponse(toCase(v)), nil
}

// MarkCaseLost implementa StrictServerInterface.
func (s *Server) MarkCaseLost(ctx context.Context, req MarkCaseLostRequestObject) (MarkCaseLostResponseObject, error) {
	v, err := s.svc.MarkLost(ctx, req.PolicyId, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return MarkCaseLost200JSONResponse(toCase(v)), nil
}

// GetRetention implementa StrictServerInterface.
func (s *Server) GetRetention(ctx context.Context, req GetRetentionRequestObject) (GetRetentionResponseObject, error) {
	from, err := kernel.ParseDate(req.Params.From)
	if err != nil {
		return nil, err
	}
	to, err := kernel.ParseDate(req.Params.To)
	if err != nil {
		return nil, err
	}
	due, renewed, err := s.svc.Retention(ctx, from, to)
	if err != nil {
		return nil, err
	}
	return GetRetention200JSONResponse{Due: due, Renewed: renewed}, nil
}

// GetRenewalSettings implementa StrictServerInterface.
func (s *Server) GetRenewalSettings(ctx context.Context, _ GetRenewalSettingsRequestObject) (GetRenewalSettingsResponseObject, error) {
	days, err := s.svc.AlertDays(ctx)
	if err != nil {
		return nil, err
	}
	return GetRenewalSettings200JSONResponse{AlertDays: days}, nil
}

// SetRenewalSettings implementa StrictServerInterface.
func (s *Server) SetRenewalSettings(ctx context.Context, req SetRenewalSettingsRequestObject) (SetRenewalSettingsResponseObject, error) {
	if err := s.svc.SetAlertDays(ctx, req.Body.AlertDays); err != nil {
		return nil, err
	}
	return SetRenewalSettings204Response{}, nil
}
