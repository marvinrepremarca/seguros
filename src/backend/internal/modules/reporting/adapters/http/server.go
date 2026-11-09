// Package reportinghttp expone reportes. Solo mapea HTTP ↔ casos de uso.
package reportinghttp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/marvinrepremarca/seguros/internal/modules/reporting/app"
	"github.com/marvinrepremarca/seguros/internal/modules/reporting/domain"
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

// GetKpis implementa StrictServerInterface.
func (s *Server) GetKpis(ctx context.Context, req GetKpisRequestObject) (GetKpisResponseObject, error) {
	from, err := kernel.ParseDate(req.Params.From)
	if err != nil {
		return nil, domain.ErrInvalidRange
	}
	to, err := kernel.ParseDate(req.Params.To)
	if err != nil {
		return nil, domain.ErrInvalidRange
	}
	k, err := s.svc.Get(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := GetKpis200JSONResponse{IssuanceDays: k.IssuanceDays, PoliciesPerClient: PoliciesPerClient{Policies: k.Policies, Clients: k.Clients}, Premiums: make([]Amount, 0, len(k.Premiums))}
	for _, p := range k.Premiums {
		out.Premiums = append(out.Premiums, Amount{Currency: p.Currency, Amount: p.Value})
	}
	if k.Conversion != nil {
		out.Conversion = &Conversion{Requests: k.Conversion.Total, Accepted: k.Conversion.Part}
	}
	if k.Retention != nil {
		out.Retention = &Retention{Due: k.Retention.Total, Renewed: k.Retention.Part}
	}
	if k.HasOverdue {
		rows := make([]AgeBucket, 0, len(k.Overdue))
		for _, b := range k.Overdue {
			rows = append(rows, AgeBucket{Bucket: AgeBucketBucket(b.Name), Currency: b.Currency, Balance: b.Balance})
		}
		out.Overdue = &rows
	}
	if k.HasCommissions {
		rows := make([]CommissionLine, 0, len(k.Commissions))
		for _, c := range k.Commissions {
			rows = append(rows, CommissionLine{Currency: c.Currency, Accrued: c.Accrued, Received: c.Received})
		}
		out.Commissions = &rows
	}
	if k.ClaimDays != nil {
		out.ClaimDays = k.ClaimDays
		rows := make([]LossRatio, 0, len(k.LossRatio))
		for _, l := range k.LossRatio {
			row := LossRatio{Currency: l.Currency, Paid: l.Paid, Premium: l.Premium}
			if l.HasPercent {
				row.Percentage = &l.Percent
			}
			rows = append(rows, row)
		}
		out.LossRatio = &rows
	}
	return out, nil
}
