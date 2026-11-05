// Package commissionshttp expone comisiones. Solo mapea HTTP ↔ casos de uso.
package commissionshttp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/commissions/app"
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

func pageArgs(cursor *string, limit *int32) (string, int32) {
	var c string
	var l int32
	if cursor != nil {
		c = *cursor
	}
	if limit != nil {
		l = *limit
	}
	return c, l
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptr(id uuid.NullUUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	return &id.UUID
}

func null(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

// ListAccruals implementa StrictServerInterface.
func (s *Server) ListAccruals(ctx context.Context, req ListAccrualsRequestObject) (ListAccrualsResponseObject, error) {
	cursor, limit := pageArgs(req.Params.Cursor, req.Params.Limit)
	rows, next, err := s.svc.ListAccruals(ctx, req.Params.Pending != nil && *req.Params.Pending, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := ListAccruals200JSONResponse{Items: make([]Accrual, 0, len(rows)), NextCursor: optional(next)}
	for _, a := range rows {
		out.Items = append(out.Items, Accrual{
			Id: a.ID, PolicyId: a.PolicyID, PolicyNumber: a.PolicyNumber, Kind: AccrualKind(a.Kind), Base: a.Base, Percentage: a.Percentage, Amount: a.Amount,
			Currency: a.Currency, AgentId: ptr(a.AgentID), AccruedOn: a.AccruedOn, MissingAgreement: a.MissingAgreement, Reconciled: a.Reconciled,
		})
	}
	return out, nil
}

// RegisterStatement implementa StrictServerInterface.
func (s *Server) RegisterStatement(ctx context.Context, req RegisterStatementRequestObject) (RegisterStatementResponseObject, error) {
	in := app.StatementInput{InsurerID: req.Body.InsurerId, Reference: req.Body.Reference, ReceivedOn: req.Body.ReceivedOn, Lines: make([]app.StatementLine, 0, len(req.Body.Lines))}
	for _, l := range req.Body.Lines {
		in.Lines = append(in.Lines, app.StatementLine{PolicyNumber: l.PolicyNumber, Amount: l.Amount})
	}
	res, err := s.svc.RegisterStatement(ctx, in)
	if err != nil {
		return nil, err
	}
	return RegisterStatement201JSONResponse{Id: res.ID, Matched: res.Matched, Discrepancies: res.Discrepancies}, nil
}

// ListDiscrepancies implementa StrictServerInterface.
func (s *Server) ListDiscrepancies(ctx context.Context, req ListDiscrepanciesRequestObject) (ListDiscrepanciesResponseObject, error) {
	cursor, limit := pageArgs(req.Params.Cursor, req.Params.Limit)
	rows, next, err := s.svc.ListDiscrepancies(ctx, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := ListDiscrepancies200JSONResponse{Items: make([]Discrepancy, 0, len(rows)), NextCursor: optional(next)}
	for _, d := range rows {
		out.Items = append(out.Items, Discrepancy{
			Id: d.ID, StatementReference: d.StatementReference, InsurerId: d.InsurerID, PolicyNumber: d.PolicyNumber, PolicyId: ptr(d.PolicyID), Reported: d.Reported, Expected: d.Expected,
		})
	}
	return out, nil
}

// ListAgentCommissions implementa StrictServerInterface.
func (s *Server) ListAgentCommissions(ctx context.Context, req ListAgentCommissionsRequestObject) (ListAgentCommissionsResponseObject, error) {
	cursor, limit := pageArgs(req.Params.Cursor, req.Params.Limit)
	rows, next, err := s.svc.ListAgentCommissions(ctx, null(req.Params.AgentId), cursor, limit)
	if err != nil {
		return nil, err
	}
	out := ListAgentCommissions200JSONResponse{Items: make([]AgentCommission, 0, len(rows)), NextCursor: optional(next)}
	for _, c := range rows {
		out.Items = append(out.Items, AgentCommission{Id: c.ID, AccrualId: c.AccrualID, AgentId: c.AgentID, Percentage: c.Percentage, Amount: c.Amount, Currency: c.Currency, MissingRate: c.MissingRate})
	}
	return out, nil
}

func toRate(r app.AgentRate) AgentRate {
	return AgentRate{Id: r.ID, AgentId: r.AgentID, LineOfBusinessId: ptr(r.LineID), Percentage: r.Percentage, ValidFrom: r.From, ValidTo: r.To}
}

// ListAgentRates implementa StrictServerInterface.
func (s *Server) ListAgentRates(ctx context.Context, _ ListAgentRatesRequestObject) (ListAgentRatesResponseObject, error) {
	rows, err := s.svc.ListRates(ctx)
	if err != nil {
		return nil, err
	}
	out := make(ListAgentRates200JSONResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, toRate(r))
	}
	return out, nil
}

// CreateAgentRate implementa StrictServerInterface.
func (s *Server) CreateAgentRate(ctx context.Context, req CreateAgentRateRequestObject) (CreateAgentRateResponseObject, error) {
	r, err := s.svc.CreateRate(ctx, app.AgentRate{AgentID: req.Body.AgentId, LineID: null(req.Body.LineOfBusinessId), Percentage: req.Body.Percentage, From: req.Body.ValidFrom, To: req.Body.ValidTo})
	if err != nil {
		return nil, err
	}
	return CreateAgentRate201JSONResponse(toRate(r)), nil
}

// GetCommissionSettings implementa StrictServerInterface.
func (s *Server) GetCommissionSettings(ctx context.Context, _ GetCommissionSettingsRequestObject) (GetCommissionSettingsResponseObject, error) {
	rows, err := s.svc.Tolerances(ctx)
	if err != nil {
		return nil, err
	}
	out := make(GetCommissionSettings200JSONResponse, 0, len(rows))
	for _, m := range rows {
		out = append(out, CommissionSettings{Currency: string(m.Currency()), Tolerance: m.Amount()})
	}
	return out, nil
}

// SetCommissionSettings implementa StrictServerInterface.
func (s *Server) SetCommissionSettings(ctx context.Context, req SetCommissionSettingsRequestObject) (SetCommissionSettingsResponseObject, error) {
	if err := s.svc.SetTolerance(ctx, req.Body.Currency, req.Body.Tolerance); err != nil {
		return nil, err
	}
	return SetCommissionSettings204Response{}, nil
}
